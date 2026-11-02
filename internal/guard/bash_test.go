package guard

import (
	"encoding/json"
	"testing"
)

func bashInput(t *testing.T, tool, command string) []byte {
	t.Helper()
	in, err := json.Marshal(map[string]any{"tool_name": tool, "tool_input": map[string]any{"command": command}})
	if err != nil {
		t.Fatal(err)
	}
	return in
}

func denyReason(t *testing.T, out string) string {
	t.Helper()
	var d map[string]map[string]string
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		t.Fatalf("not a decision %q %v", out, err)
	}
	h := d["hookSpecificOutput"]
	if h["hookEventName"] != "PreToolUse" || h["permissionDecision"] != "deny" || h["permissionDecisionReason"] == "" {
		t.Fatalf("not a denial %q", out)
	}
	return h["permissionDecisionReason"]
}

type bashCases struct {
	tool     string
	commands []string
}

func expectBash(t *testing.T, wantDeny bool, cases []bashCases) {
	t.Helper()
	for _, c := range cases {
		for _, command := range c.commands {
			t.Run(c.tool+" "+command, func(t *testing.T) {
				out := Bash(bashInput(t, c.tool, command))
				if wantDeny {
					denyReason(t, out)
				} else if out != "" {
					t.Fatalf("denied %s", out)
				}
			})
		}
	}
}

func TestBashDeniesEndingAProcessByItsName(t *testing.T) {
	expectBash(t, true, []bashCases{
		{"Bash", []string{
			"taskkill /IM node.exe /F",
			"taskkill //IM node.exe",
			"taskkill -IM node.exe",
			`taskkill /FI "IMAGENAME eq node.exe"`,
			"pkill -f vite",
			"killall node",
			"Stop-Process -Name node -Force",
			"Stop-Process -Na node",
			"Stop-Process node",
			"Stop-Process -ProcessName node",
			"Stop-Process -InputObject (Get-Process node)",
			"spps node",
			"kill node",
			"Get-Process node | Stop-Process",
			"gps node | spps",
			"Get-Process node | kill",
			"Get-Process node | % Kill",
			"Get-Process node | ForEach-Object { $_.Kill() }",
			"(Get-Process node) | Stop-Process",
			"(Get-Process node).Kill()",
			"Stop-Process -Id (Get-Process node).Id",
			"kill $(Get-Process node).Id",
			"kill $(pgrep node)",
			`kill "$(pgrep node)"`,
			"kill $(ps aux | grep node | awk '{print $2}')",
			"pgrep -f vite | xargs kill",
			"pgrep -f vite | xargs -I{} kill {}",
			"ps aux | grep node | awk '{print $2}' | xargs kill -9",
			"wmic process where name='node.exe' delete",
			"wmic process where name='node.exe' call terminate",
			`bash -c "pkill node"`,
			"pwsh -Command 'Stop-Process -Name node'",
			"cmd /c taskkill /IM node.exe /F",
			"powershell -NoProfile -Command Stop-Process -Name node -Force",
			"pwsh -c pkill node",
			"powershell -EncodedCommand U3RvcC1Qcm9jZXNz",
			`eval "pkill node"`,
			`iex "Stop-Process -Name node"`,
			"echo pkill node | bash",
			`bash <<< "pkill node"`,
			"cd D:/x && pkill node",
			"ls; pkill node",
			"sudo pkill node",
			"sudo -n pkill node",
			"env -i pkill node",
			"FOO=1 pkill node",
			"/usr/bin/pkill node",
			`pk\ill node`,
			"(pkill node)",
			"{ pkill node; }",
			"if true; then pkill node; fi",
			"for p in a; do pkill $p; done",
			"env pkill node",
			"timeout 5 pkill node",
			"echo node | xargs pkill",
			"echo node | xargs -t pkill",
			"echo node | xargs -t kill node",
			"echo node | xargs -n1 killall",
			"x=$(pkill node)",
			"echo $(pkill node)",
			`echo "$(killall node)"`,
			"echo `pkill node`",
			"C:/Windows/System32/taskkill.exe /IM node.exe",
			"bash <<'EOF'\npkill node\nEOF",
		}},
		{"PowerShell", []string{
			"Stop-Process -Name node",
			"Get-Process node | Stop-Process",
			`C:\Windows\System32\taskkill.exe /IM node.exe`,
			`& "C:\Windows\System32\taskkill.exe" /IM node.exe`,
			`echo "C:\tmp\"; Stop-Process -Name node`,
			"ForEach-Object { Stop-Process -Name $_ }",
			"foreach ($p in $names) { Stop-Process -Name $p }",
			"if ($ok) { Write-Host ok } else { Stop-Process -Name node }",
			"try { x } catch { Stop-Process -Name node }",
			`Get-CimInstance Win32_Process -Filter "name='node.exe'" | Invoke-CimMethod -MethodName Terminate`,
			"cmd /c taskkill /IM node.exe",
			"kill -Name node",
		}},
	})
}

func TestBashAllowsEndingAProcessByIDPathOrPort(t *testing.T) {
	expectBash(t, false, []bashCases{
		{"Bash", []string{
			"taskkill /PID 1234 /F",
			"Stop-Process -Id 1234",
			"Stop-Process -Id 1234 -Force",
			"Stop-Process 1234",
			"Stop-Process -Force 1234",
			"Stop-Process 1234 -ErrorAction SilentlyContinue",
			"Stop-Process $pid -ErrorAction SilentlyContinue",
			"Stop-Process $proc.Id",
			"spps 1234",
			"Get-Process -Id 1234 | Stop-Process",
			"$p = Start-Process x -PassThru; Stop-Process -InputObject $p",
			"kill 1234",
			"kill -9 1234",
			"kill -s TERM 1234",
			"kill -s hup 1234",
			"kill -n 9 1234",
			"kill $PID",
			"kill $(cat server.pid)",
			"kill %1",
			"ps aux | grep node",
			"Get-Process node",
			"pgrep -f vite",
			"pwsh -File script.ps1",
			"sudo -n true",
		}},
		{"PowerShell", []string{
			"Stop-Process -Id 1234",
			"Get-Process -Id 1234 | Stop-Process",
			"kill 1234",
			"Stop-Process -Id 1234 -ErrorAction SilentlyContinue",
		}},
	})
}

func TestBashReadsAForbiddenWordInsideQuotedTextAsData(t *testing.T) {
	expectBash(t, false, []bashCases{
		{"Bash", []string{
			`git commit -m "tools: hooks deny pkill and taskkill /IM"`,
			`git commit -m "tools: pgrep no longer needs kill"`,
			`git commit -m "fix (kill the flake)"`,
			"git commit -m 'tools: deny `pkill node`'",
			"gh pr create --body \"Denies \\`pkill\\` and \\`kill node\\`\"",
			"grep -rn killall docs",
			"grep -rn 'Stop-Process -Name' tools",
			`grep -rn "pgrep.*kill" docs`,
			`echo "pidof then kill"`,
			"echo '$(pkill node)'",
			`echo '{"tool_input":{"command":"pkill node"}}' | node tools/hooks/guard-bash.mjs`,
			"node --test tools/hooks/guard.test.mjs",
			"ls -la",
			"git commit -m \"$(cat <<'EOF'\ntools: hooks\n\npnpm eval --scope smoke is denied without --budget; pkill node too.\nEOF\n)\"",
			"cat > notes.md <<'EOF'\npkill node\nkill node\npnpm eval --scope full\nEOF",
		}},
		{"PowerShell", []string{
			`git commit -m "fix (kill the flake)"`,
			"Write-Host 'pkill node'",
		}},
	})
}

func TestBashAllowsUnparseableOrEmptyInput(t *testing.T) {
	empty, err := json.Marshal(map[string]any{"tool_name": "Bash", "tool_input": map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range [][]byte{[]byte(""), []byte("{not json"), empty} {
		t.Run(string(in), func(t *testing.T) {
			if out := Bash(in); out != "" {
				t.Fatalf("denied %s", out)
			}
		})
	}
}

func TestBashNamesTheRuleItDeniesBy(t *testing.T) {
	for _, c := range []struct{ command, reason string }{
		{"powershell -EncodedCommand U3RvcC1Qcm9jZXNz", encodedReason},
		{"pkill node", killReason},
	} {
		t.Run(c.command, func(t *testing.T) {
			if got := denyReason(t, Bash(bashInput(t, "Bash", c.command))); got != c.reason {
				t.Fatalf("reason %q, want %q", got, c.reason)
			}
		})
	}
}

func TestBashReadsAHeredocOnlyWhereBashReadsCode(t *testing.T) {
	expectBash(t, true, []bashCases{{"Bash", []string{
		"cat <<< \"x\"\npkill node",
		"cat <<<x\npkill node",
		"echo \"<<EOF\"\npkill node",
		"echo '<<EOF'\npkill node",
		"printf '%s' \"one\ntwo <<EOF\"\npkill node",
		"echo \\<<EOF\npkill node",
		"# a heredoc <<EOF in a comment\npkill node",
	}}})
	expectBash(t, false, []bashCases{{"Bash", []string{
		"cat <<EOF\npkill node\nEOF",
		"cat <<-EOF\n\tpkill node\n\tEOF",
		"git commit -m \"$(cat <<'EOF'\nfix: pkill node is denied\nEOF\n)\"",
		"echo `cat <<EOF\npkill node\nEOF\n`",
		"# don't stop here\ncat <<EOF\npkill node\nEOF",
		"x=$( (cat <<EOF\npkill node\nEOF\n) )",
	}}})
}

func TestBashDeniesAListerThatFeedsALoopWhoseBodyKills(t *testing.T) {
	expectBash(t, true, []bashCases{{"Bash", []string{
		"for p in $(pgrep node); do kill $p; done",
		"for p in `pidof vite`; do kill -9 $p; done",
		"pgrep node | while read p; do kill $p; done",
		"pgrep -f vite | while read -r p\ndo\n  kill \"$p\"\ndone",
		"while read p; do kill $p; done < <(pgrep node)",
		"ps aux | grep node | while read u p rest; do kill $p; done",
		"cd /tmp && for p in $(pgrep node); do echo $p; kill $p; done",
		"for p in $(pgrep node); do if true; then kill $p; fi; done",
	}}})
}

func TestBashAllowsLoopsThatListOrKillAlone(t *testing.T) {
	expectBash(t, false, []bashCases{{"Bash", []string{
		"pgrep x || echo nothing to kill",
		"for p in $(pgrep node); do echo $p; done",
		"pgrep node | while read p; do echo $p; done",
		"for i in 1 2 3; do kill $i; done",
		"while true; do sleep 1; done; pgrep node",
		"for f in *.log; do pgrep -f $f; done; kill 1234",
	}}})
}
