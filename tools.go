package main

var (
	// TODO: form that message based on existing funcs
	systemMsg = `You're a helpful assistant.
# Tools
You can do functions call if needed.
Your current tools:
<tools>
{
"name":"get_id",
"args": "username"
}
</tools>
To make a function call return a json object within __tool_call__ tags;
Example:
__tool_call__
{
"name":"get_id",
"args": "Adam"
}
__tool_call__
When making function call avoid typing anything else. 'tool' user will respond with the results of the call.
After that you are free to respond to the user.
`
)

func memorize(topic, info string) {
	//
}

func recall(topic string) string {
	//
	return ""
}

func recallTopics() []string {
	return []string{}
}

func fullMemoryLoad() {}

// predifine funcs
func getUserDetails(id ...string) map[string]any {
	// db query
	// return DB[id[0]]
	return map[string]any{
		"username":   "fm11",
		"id":         24983,
		"reputation": 911,
		"balance":    214.73,
	}
}

type fnSig func(...string) map[string]any

var fnMap = map[string]fnSig{
	"get_id": getUserDetails,
}
