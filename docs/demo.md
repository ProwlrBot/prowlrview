# prowlrview — demo runs

## Pipe nuclei into the live graph
```sh
nuclei -jsonl -l hosts.txt -severity high,critical | prowlrview pipe
```

## Watch a results directory
```sh
prowlrview watch ~/hunts/target.com/
```

## Pipe flaw (Crystal static analyzer) findings
```sh
flaw scan --json ./src | prowlrview pipe
```

## Chain recon into one graph
```sh
subfinder -d target.com -silent -oJ \
  | httpx -json -silent \
  | tee >(katana -jsonl) \
  | prowlrview pipe
```

## Sample event (paste into stdin)
```json
{"template-id":"cve-2024-1234","info":{"name":"RCE in widget","severity":"critical"},"host":"api.target.com","matched-at":"https://api.target.com/v1/upload"}
{"url":"https://api.target.com/admin","input":"api.target.com","status_code":200,"title":"Admin","tech":["Nginx","WordPress"]}
{"host":"beta.target.com","source":"crtsh"}
```

## Keys
| key | action |
|-----|--------|
| `q` | quit |
| `t` | cycle theme |
| `f` | toggle follow |
| `r` | refresh |
| `?` | help |
