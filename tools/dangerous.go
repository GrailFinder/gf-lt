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
	if strings.HasPrefix(cmdLower, "dd ") {
		return true, "dd (dangerous disk write)"
	}
	if strings.HasPrefix(cmdLower, "shred ") {
		return true, "shred (secure delete)"
	}
	if strings.HasPrefix(cmdLower, "mkfs") {
		return true, "mkfs (format filesystem)"
	}
	if strings.HasPrefix(cmdLower, "fdisk ") {
		return true, "fdisk (partition manipulation)"
	}
	if strings.HasPrefix(cmdLower, "shutdown ") {
		return true, "shutdown (system shutdown)"
	}
	if cmdLower == "shutdown" {
		return true, "shutdown (system shutdown)"
	}
	if strings.HasPrefix(cmdLower, "poweroff") {
		return true, "poweroff (system power off)"
	}
	if strings.HasPrefix(cmdLower, "reboot") {
		return true, "reboot (system reboot)"
	}
	if strings.HasPrefix(cmdLower, "iptables ") {
		return true, "iptables (firewall manipulation)"
	}
	if strings.HasPrefix(cmdLower, "ufw ") {
		return true, "ufw (firewall manipulation)"
	}
	if strings.HasPrefix(cmdLower, "chmod -r ") || strings.HasPrefix(cmdLower, "chmod --recursive ") {
		return true, "chmod -R (recursive permission change)"
	}
	if strings.HasPrefix(cmdLower, "chown -r ") || strings.HasPrefix(cmdLower, "chown --recursive ") {
		return true, "chown -R (recursive ownership change)"
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
