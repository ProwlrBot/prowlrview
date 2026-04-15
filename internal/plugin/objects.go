package plugin

import (
	"bytes"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"

	lua "github.com/yuin/gopher-lua"
)

// Request is the userdata object exposed to plugins as `req`.
type Request struct {
	R       *http.Request
	Blocked bool
	Reason  string
}

// Response is exposed as `resp`.
type Response struct {
	R        *http.Response
	bodyOnce sync.Once
	body     []byte
}

func (rq *Request) bodyBytes() []byte {
	if rq.R.Body == nil {
		return nil
	}
	b, _ := io.ReadAll(rq.R.Body)
	rq.R.Body = io.NopCloser(bytes.NewReader(b))
	return b
}

func (rs *Response) bodyBytes() []byte {
	rs.bodyOnce.Do(func() {
		if rs.R.Body == nil {
			return
		}
		b, _ := io.ReadAll(rs.R.Body)
		rs.R.Body = io.NopCloser(bytes.NewReader(b))
		rs.body = b
	})
	return rs.body
}

const (
	reqTypeName  = "pv.request"
	respTypeName = "pv.response"
)

func registerTypes(L *lua.LState) {
	rq := L.NewTypeMetatable(reqTypeName)
	L.SetField(rq, "__index", L.SetFuncs(L.NewTable(), map[string]lua.LGFunction{
		"header":       reqHeader,
		"set_header":   reqSetHeader,
		"block":        reqBlock,
		"replace_body": reqReplaceBody,
		"body":         reqBody,
	}))

	rs := L.NewTypeMetatable(respTypeName)
	L.SetField(rs, "__index", L.SetFuncs(L.NewTable(), map[string]lua.LGFunction{
		"header":  respHeader,
		"matches": respMatches,
		"body":    respBody,
	}))
}

// pushRequest builds a Lua table backed by userdata so plugins can use both
// field access (req.host, req.path, req.method, req.url, req.headers) AND
// method calls (req:header, req:block, req:replace_body).
func pushRequest(L *lua.LState, r *http.Request) (*lua.LTable, *Request) {
	req := &Request{R: r}
	ud := L.NewUserData()
	ud.Value = req
	L.SetMetatable(ud, L.GetTypeMetatable(reqTypeName))

	t := L.NewTable()
	L.SetField(t, "method", lua.LString(r.Method))
	L.SetField(t, "host", lua.LString(r.URL.Host))
	L.SetField(t, "path", lua.LString(r.URL.Path+qMark(r)))
	L.SetField(t, "url", lua.LString(r.URL.String()))
	L.SetField(t, "headers", headersTable(L, r.Header))
	L.SetField(t, "_ud", ud)
	// Bind convenience methods at table level so both `req:header(...)` and
	// `req.header(req, ...)` work without surprises for Lua beginners.
	L.SetField(t, "header", L.NewClosure(reqHeaderFromTable, ud))
	L.SetField(t, "set_header", L.NewClosure(reqSetHeaderFromTable, ud))
	L.SetField(t, "block", L.NewClosure(reqBlockFromTable, ud))
	L.SetField(t, "replace_body", L.NewClosure(reqReplaceBodyFromTable, ud))
	L.SetField(t, "body", L.NewClosure(reqBodyFromTable, ud))
	return t, req
}

func pushResponse(L *lua.LState, resp *http.Response) (*lua.LTable, *Response) {
	rs := &Response{R: resp}
	ud := L.NewUserData()
	ud.Value = rs
	L.SetMetatable(ud, L.GetTypeMetatable(respTypeName))

	t := L.NewTable()
	L.SetField(t, "status", lua.LNumber(resp.StatusCode))
	L.SetField(t, "url", lua.LString(resp.Request.URL.String()))
	L.SetField(t, "headers", headersTable(L, resp.Header))
	body := rs.bodyBytes()
	L.SetField(t, "body", lua.LString(body))
	L.SetField(t, "_ud", ud)
	L.SetField(t, "header", L.NewClosure(respHeaderFromTable, ud))
	L.SetField(t, "matches", L.NewClosure(respMatchesFromTable, ud))
	return t, rs
}

func headersTable(L *lua.LState, h http.Header) *lua.LTable {
	t := L.NewTable()
	for k, v := range h {
		if len(v) > 0 {
			L.SetField(t, strings.ToLower(k), lua.LString(v[0]))
		}
	}
	return t
}

func qMark(r *http.Request) string {
	if r.URL.RawQuery != "" {
		return "?" + r.URL.RawQuery
	}
	return ""
}

// --- userdata method receivers (used when plugin calls req:header on ud) ---

func checkReq(L *lua.LState, n int) *Request {
	ud := L.CheckUserData(n)
	r, ok := ud.Value.(*Request)
	if !ok {
		L.ArgError(n, "expected request")
	}
	return r
}
func checkResp(L *lua.LState, n int) *Response {
	ud := L.CheckUserData(n)
	r, ok := ud.Value.(*Response)
	if !ok {
		L.ArgError(n, "expected response")
	}
	return r
}

func reqHeader(L *lua.LState) int {
	r := checkReq(L, 1)
	L.Push(lua.LString(r.R.Header.Get(L.CheckString(2))))
	return 1
}
func reqSetHeader(L *lua.LState) int {
	r := checkReq(L, 1)
	r.R.Header.Set(L.CheckString(2), L.CheckString(3))
	return 0
}
func reqBlock(L *lua.LState) int {
	r := checkReq(L, 1)
	r.Blocked = true
	r.Reason = L.OptString(2, "blocked by plugin")
	return 0
}
func reqReplaceBody(L *lua.LState) int {
	r := checkReq(L, 1)
	b := L.CheckString(2)
	r.R.Body = io.NopCloser(strings.NewReader(b))
	r.R.ContentLength = int64(len(b))
	return 0
}
func reqBody(L *lua.LState) int {
	r := checkReq(L, 1)
	L.Push(lua.LString(r.bodyBytes()))
	return 1
}
func respHeader(L *lua.LState) int {
	r := checkResp(L, 1)
	L.Push(lua.LString(r.R.Header.Get(L.CheckString(2))))
	return 1
}
func respMatches(L *lua.LState) int {
	r := checkResp(L, 1)
	pat := L.CheckString(2)
	re, err := regexp.Compile(pat)
	if err != nil {
		// fall back to plain substring if the Lua-pattern-ish string isn't valid Go regex
		L.Push(lua.LBool(bytes.Contains(r.bodyBytes(), []byte(pat))))
		return 1
	}
	L.Push(lua.LBool(re.Match(r.bodyBytes())))
	return 1
}
func respBody(L *lua.LState) int {
	r := checkResp(L, 1)
	L.Push(lua.LString(r.bodyBytes()))
	return 1
}

// --- table closures: first upvalue is the userdata, args shift by one ---

func udFromClosure(L *lua.LState) *lua.LUserData {
	return L.CheckUserData(lua.UpvalueIndex(1))
}
func reqFromClosure(L *lua.LState) *Request {
	r, _ := udFromClosure(L).Value.(*Request)
	return r
}
func respFromClosure(L *lua.LState) *Response {
	r, _ := udFromClosure(L).Value.(*Response)
	return r
}

func reqHeaderFromTable(L *lua.LState) int {
	// when called as req:header(k), first arg is the table (self) — shift.
	_ = L.CheckTable(1)
	L.Push(lua.LString(reqFromClosure(L).R.Header.Get(L.CheckString(2))))
	return 1
}
func reqSetHeaderFromTable(L *lua.LState) int {
	_ = L.CheckTable(1)
	reqFromClosure(L).R.Header.Set(L.CheckString(2), L.CheckString(3))
	return 0
}
func reqBlockFromTable(L *lua.LState) int {
	_ = L.CheckTable(1)
	r := reqFromClosure(L)
	r.Blocked = true
	r.Reason = L.OptString(2, "blocked by plugin")
	return 0
}
func reqReplaceBodyFromTable(L *lua.LState) int {
	_ = L.CheckTable(1)
	r := reqFromClosure(L)
	b := L.CheckString(2)
	r.R.Body = io.NopCloser(strings.NewReader(b))
	r.R.ContentLength = int64(len(b))
	return 0
}
func reqBodyFromTable(L *lua.LState) int {
	_ = L.CheckTable(1)
	L.Push(lua.LString(reqFromClosure(L).bodyBytes()))
	return 1
}
func respHeaderFromTable(L *lua.LState) int {
	_ = L.CheckTable(1)
	L.Push(lua.LString(respFromClosure(L).R.Header.Get(L.CheckString(2))))
	return 1
}
func respMatchesFromTable(L *lua.LState) int {
	_ = L.CheckTable(1)
	r := respFromClosure(L)
	pat := L.CheckString(2)
	re, err := regexp.Compile(pat)
	if err != nil {
		L.Push(lua.LBool(bytes.Contains(r.bodyBytes(), []byte(pat))))
		return 1
	}
	L.Push(lua.LBool(re.Match(r.bodyBytes())))
	return 1
}
