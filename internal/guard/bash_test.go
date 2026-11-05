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

func TestBashDeniesSignallingEveryProcess(t *testing.T) {
	expectBash(t, true, []bashCases{{"Bash", []string{
		"kill -9 -1",
		"kill -KILL -1",
		"kill -1 -1",
		"kill -- -1",
		"kill -9 -- -1",
		"kill -s TERM -1",
		"kill -n 9 -1",
		"kill 1234 -1",
		"/bin/kill -9 -1",
		"sudo kill -9 -1",
		"bash -c 'kill -9 -1'",
		"true && kill -TERM -1",
	}}})
	expectBash(t, false, []bashCases{{"Bash", []string{
		"kill -1 1234",
		"kill -HUP 1234",
		"kill -1",
		"kill -l",
		"kill -9 -- -4321",
		"kill -- -4321",
	}}})
}

func TestBashNamesTheRuleItDeniesBy(t *testing.T) {
	for _, c := range []struct{ command, reason string }{
		{"powershell -EncodedCommand U3RvcC1Qcm9jZXNz", encodedReason},
		{"pkill node", killReason},
		{"kill -9 -1", everyReason},
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

func TestBashReadsTheAmpersandOfARedirectAsPartOfTheRedirect(t *testing.T) {
	expectBash(t, true, []bashCases{{"Bash", []string{
		"pgrep -f vite 2>&1 | xargs kill",
		"pgrep node |& xargs kill",
		"pgrep node 2>&1 | while read p; do kill $p; done",
		"pidof node 2>&1|xargs kill",
		"pgrep node &>/dev/null | xargs kill",
		"pgrep node >&2 | xargs kill",
		"pgrep node <&0 | xargs kill",
		"pgrep node |& tee pids | xargs kill",
		"for p in $(pgrep node 2>&1); do kill $p; done",
		"kill node &>/dev/null",
		"kill node&>/dev/null",
		"kill node 2>&1",
		"kill >| out node",
		`echo ">"& pkill node`,
		`echo \>& pkill node`,
		`echo \>|pkill node`,
	}}})
	expectBash(t, false, []bashCases{{"Bash", []string{
		"kill 1234 2>&1",
		"kill $pid &>/dev/null",
		"kill 1234 >&2",
		"pgrep node >| pids.txt",
		"pgrep -f vite 2>&1",
	}}})
}

func TestBashReadsACommentAsNothingToRun(t *testing.T) {
	expectBash(t, false, []bashCases{{"Bash", []string{
		"kill 1234 # the dev server",
		"kill $(cat .vite.pid) # stop vite",
		"kill 1234 # not $(pgrep node)",
		"kill 1234 # not (pgrep node)",
		"echo ok # don't pkill node",
		"# pkill node\nkill 1234",
		"kill 1234;# pkill node",
	}}})
	expectBash(t, true, []bashCases{{"Bash", []string{
		"pkill node # stop it",
		"kill $(pgrep node) # stop it",
		"true # one\npkill node",
		"echo hi # don't\npkill node",
		"echo a#b; pkill node",
		"echo $#; pkill node",
		"echo ${#x}; pkill node",
		`echo ""#x; pkill node`,
		"echo '#'; pkill node",
		`echo \#x; pkill node`,
		"true #; pkill node",
	}}})
}

func TestBashCountsAListerOrAKillOnlyAsTheCommandWord(t *testing.T) {
	expectBash(t, false, []bashCases{{"Bash", []string{
		"grep -rn pgrep tools/kill-port.sh",
		"git diff -- src/pidof.ts src/kill.ts",
		"cat src/kill.ts src/pgrep.ts",
		"git log --grep kill | grep pgrep",
		"command -v pkill",
		"command -V pkill",
		"command -pv pkill",
		"ps aux | grep -i terminate",
	}}})
	expectBash(t, true, []bashCases{{"Bash", []string{
		"command pkill node",
		"command -p pkill node",
		"command -v pkill && pkill node",
		"ps aux | grep terminate | xargs kill",
		"pgrep node | grep -v x | xargs kill",
		"FOO=1 pgrep -f vite | xargs kill",
		"sudo -n pgrep -f vite | xargs sudo kill",
		"nohup kill $(pgrep node)",
		"time kill $(pidof vite)",
		"exec kill $(pgrep node)",
		"xargs kill < <(pgrep node)",
	}}})
}

func TestBashAllowsPgrepThatPicksTheChildrenOfAParentID(t *testing.T) {
	expectBash(t, false, []bashCases{{"Bash", []string{
		"pgrep -P 4321 | xargs kill",
		"pgrep -P4321 | xargs kill",
		"pgrep --parent 4321 | xargs kill",
		"pgrep --parent=4321 | xargs kill",
		"pgrep -P 4321 -u chris | xargs kill",
		"kill $(pgrep -P 4321)",
		"for p in $(pgrep -P $$); do kill $p; done",
	}}})
	expectBash(t, true, []bashCases{{"Bash", []string{
		"pgrep -f name | xargs kill",
		"pgrep -P 4321 node | xargs kill",
		"pgrep -P 4321 -f vite | xargs kill",
		"pgrep -P 4321 -- node | xargs kill",
		"pgrep -v -P 4321 | xargs kill",
		"pgrep -vP 4321 | xargs kill",
		"pgrep -P 4321 --inverse | xargs kill",
		"pgrep -u chris | xargs kill",
		"pkill -P 4321",
	}}})
}

func TestBashDeniesPgrepByTheParentOfEveryService(t *testing.T) {
	expectBash(t, true, []bashCases{{"Bash", []string{
		"pgrep -P 1 | xargs kill",
		"pgrep -P 0 | xargs kill",
		"pgrep -P1 | xargs kill",
		"pgrep -P 01 | xargs kill",
		"pgrep --parent=1 | xargs kill",
		"pgrep -P 4321,1 | xargs kill",
		"kill $(pgrep -P 1)",
	}}})
	expectBash(t, false, []bashCases{{"Bash", []string{
		"pgrep -P $$ | xargs kill",
		"pgrep -P $PPID | xargs kill",
		"pgrep -P 4321,4322 | xargs kill",
	}}})
}

func TestBashReadsTheCommandOfEveryWrapperItKnows(t *testing.T) {
	expectBash(t, true, []bashCases{{"Bash", []string{
		"setsid kill $(pgrep node)",
		"setsid -f pkill node",
		"stdbuf -oL kill $(pgrep node)",
		"stdbuf --output=L pkill node",
		"nice -n 5 kill $(pgrep node)",
		"ionice -c3 kill $(pgrep node)",
		"chrt -f 10 pkill node",
		"taskset 03 pkill node",
		"taskset -c 0-3 kill $(pidof node)",
		"timeout 5 kill $(pgrep node)",
		"env kill $(pgrep node)",
		"env -S 'pkill node'",
		"env --split-string='pkill node'",
		"doas kill $(pgrep node)",
		"sudo --user='root' pkill node",
		"busybox pkill node",
		"busybox kill $(pidof node)",
		"busybox pgrep node | xargs kill",
		"sshpass -p pw ssh localhost pkill node",
		"watch 'pgrep x | xargs kill'",
		"watch -n 5 'pkill node'",
	}}})
	expectBash(t, false, []bashCases{{"Bash", []string{
		"chrt -p 1234",
		"taskset -p 03 1234",
		"busybox ls",
		"busybox grep pgrep tools/kill-port.sh",
		"setsid kill 1234",
	}}})
}

func TestBashReadsParallelAsEveryCommandItMayRun(t *testing.T) {
	expectBash(t, true, []bashCases{{"Bash", []string{
		"parallel kill ::: $(pgrep node)",
		"parallel kill ::: node vite",
		"parallel 'kill {}' ::: node",
		"parallel -j4 kill ::: node",
		"parallel --joblog log kill ::: node",
		"parallel --unknown x kill ::: node",
		"pgrep node | parallel kill",
	}}})
	expectBash(t, false, []bashCases{{"Bash", []string{
		"parallel kill ::: 1234 5678",
		"parallel 'kill {}' ::: 1234",
		"pgrep -P 4321 | parallel kill",
		"parallel gzip ::: *.log",
	}}})
}

func TestBashReadsAnSSHCommandAsTheShellAtTheOtherEndWould(t *testing.T) {
	expectBash(t, true, []bashCases{{"Bash", []string{
		"ssh localhost kill $(pgrep node)",
		"ssh localhost pkill node",
		"ssh localhost 'pkill node'",
		`ssh localhost "pgrep -f vite | xargs kill"`,
		`ssh localhost 'kill $(pgrep node)'`,
		"ssh -p 22 localhost pkill node",
		"ssh -p22 localhost pkill node",
		"ssh -tp 22 localhost pkill node",
		"ssh localhost -t pkill node",
		"ssh -Z localhost pkill node",
		`ssh -o RemoteCommand="pkill node" host`,
		`ssh -o ProxyCommand="pkill node" host true`,
		"autossh -M 0 host pkill node",
		"pgrep node | ssh host xargs kill",
	}}})
	expectBash(t, false, []bashCases{{"Bash", []string{
		"ssh localhost kill 1234",
		"ssh localhost ls -la",
		"ssh localhost grep -rn pgrep tools/kill-port.sh",
		"ssh localhost",
	}}})
}

func TestBashDeniesAKillBesideAListerInTheArgumentsOfAnyCommand(t *testing.T) {
	expectBash(t, true, []bashCases{{"Bash", []string{
		"strace kill $(pgrep node)",
		"flock /tmp/lock kill $(pgrep node)",
		"unbuffer kill $(pgrep node)",
		`find . -exec kill {} \; $(pgrep x)`,
		"tee >(xargs kill) < <(pgrep node)",
		"Get-CimInstance Win32_Process | Invoke-CimMethod -MethodName Terminate",
	}}})
	expectBash(t, false, []bashCases{{"Bash", []string{
		"strace -f node server.js",
		"diff <(pgrep a) <(pgrep b)",
	}}})
}

func TestBashGoesOnToTheNextLineAfterAPipe(t *testing.T) {
	expectBash(t, true, []bashCases{{"Bash", []string{
		"pgrep -f vite |\nxargs kill",
		"pgrep -f vite | \n  xargs kill",
		"pgrep -f vite |\n\n  xargs kill",
		"pgrep -f vite | # the ids\n  xargs kill",
		"pgrep -f vite |&\nxargs kill",
		"pgrep -f vite \\\n| xargs kill",
		"pgrep node |\nwhile read p; do kill $p; done",
		"true &&\npkill node",
		"false ||\npkill node",
	}}})
	expectBash(t, false, []bashCases{{"Bash", []string{
		"pgrep -f vite |\nwc -l",
		"pgrep -f vite\nkill 1234",
	}}})
}

func TestBashReadsASubstitutionAsOneWord(t *testing.T) {
	expectBash(t, true, []bashCases{{"Bash", []string{
		"kill $(\npgrep node\n)",
		"kill $(\n  pgrep -f vite # the dev server\n)",
		"x=$(true a) pkill node",
		`x="$(true a)" pkill node`,
		"x=${y:-a b} pkill node",
		"x=`true a` pkill node",
		`kill "$(echo "$(pgrep node)")"`,
	}}})
	expectBash(t, false, []bashCases{{"Bash", []string{
		"x=$(cat a.pid) kill $x",
		`kill $(cat "$(echo pidfile)")`,
		"kill $((1000 + 234))",
	}}})
}

func TestBashReadsARedirectApartFromTheWordItIsGluedTo(t *testing.T) {
	expectBash(t, true, []bashCases{{"Bash", []string{
		"pkill>/dev/null node",
		"pkill 2>/dev/null node",
		"2>/dev/null pkill node",
		">/dev/null pkill node",
		"kill node>/dev/null",
		"kill node2>/dev/null",
		"kill node < in.txt",
		`bash<<<"pkill node"`,
		`bash <<<"pkill node"`,
	}}})
	expectBash(t, false, []bashCases{{"Bash", []string{
		"kill 1234 > out.log",
		"kill 1234 >> out.log",
		"kill 1234 2> err.log",
		"kill 1234 &> all.log",
		"kill 1234 < in.txt",
		"kill 1234 > node",
		"kill $PID > /dev/null 2>&1",
		`cat <<<"pkill node"`,
	}}})
}

func TestBashDeniesAKillGivenAWordThatIsNoProcessID(t *testing.T) {
	expectBash(t, true, []bashCases{{"Bash", []string{
		"echo node | xargs kill",
		"echo node vite | xargs kill",
		"echo -n node | xargs kill",
		`printf '%s\n' node | xargs kill`,
		`printf 'node\n' | xargs kill`,
		"echo node | while read p; do kill $p; done",
		"xargs kill <<< node",
		"while read p; do kill $p; done <<< node",
		"for p in node vite; do kill $p; done",
		`for p in node; do kill -9 "$p"; done`,
		"n=node; kill $n",
		"n=node; kill ${n}",
		"export N=node; kill $N",
		"kill $(echo node)",
	}}})
	expectBash(t, false, []bashCases{{"Bash", []string{
		"echo 1234 | xargs kill",
		"echo $PID | xargs kill",
		`echo "$PIDS" | xargs kill`,
		`printf '%s\n' 1234 | xargs kill`,
		"xargs kill <<< 1234",
		"for i in 1 2 3; do kill $i; done",
		"for f in *.pid; do kill $(cat $f); done",
		"for p in $(cat pids); do kill $p; done",
		"PID=$(cat .pid); kill $PID",
		"n=node; n=1234; kill $n",
		"kill $(echo 1234)",
		"cat pids | xargs kill",
		"lsof -ti :3000 | xargs kill",
		"jobs -p | xargs kill",
		`echo "stopping server" && kill $(cat server.pid)`,
		`kill $(cat server.pid) || echo "not running"`,
		`echo "Killing $(cat pid)"; kill $(cat pid)`,
	}}})
}
