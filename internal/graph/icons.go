package graph

func KindIcon(kind string) string {
	switch kind {
	case "host":
		return "🌐"
	case "endpoint":
		return "🔗"
	case "finding":
		return "⚠"
	case "asset":
		return "📦"
	case "flow":
		return "⇄"
	default:
		return "·"
	}
}
