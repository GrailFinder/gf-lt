package tools

import (
	"strings"
)

var ConfirmChan = make(chan ConfirmRequest, 1)

type ConfirmRequest struct {
	ToolName string
	Command  string
	ToolArgs map[string]string
	Result   chan<- bool
}

func IsDangerousCommand(name string, args map[string]string) (bool, string) {
	if name != "bash" {
		return false, ""
	}
	cmd := args["command"]
	if cmd == "" {
		return false, ""
	}
	cmdLower := strings.ToLower(cmd)

	if strings.HasPrefix(cmdLower, "rm ") {
		return true, "rm (delete file)"
	}
	if strings.HasPrefix(cmdLower, "git push") {
		return true, "git push (remote change)"
	}
	if strings.HasPrefix(cmdLower, "sudo ") {
		return true, "sudo (privilege escalation)"
	}
	return false, ""
}

func RequestConfirmation(req ConfirmRequest) bool {
	resultCh := make(chan bool, 1)
	extendedReq := ConfirmRequest{
		ToolName: req.ToolName,
		Command:  req.Command,
		ToolArgs: req.ToolArgs,
		Result:   resultCh,
	}
	ConfirmChan <- extendedReq
	return <-resultCh
}
