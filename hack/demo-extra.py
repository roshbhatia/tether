import json
import os
import pathlib
import shutil
import subprocess
import sys
import tempfile
ROOT = pathlib.Path(__file__).resolve().parents[1]
NAME = sys.argv[1]
META = json.loads((ROOT / 'extras' / NAME / 'demo.json').read_text())
TOOL = META['core']
OLD = 'package auth\nimport "strings"\nfunc ValidToken(header string) bool { return strings.Contains(header, "Bearer ") }\n'
NEW = 'package auth\nimport "strings"\nfunc ValidToken(header string) bool {\n if !strings.HasPrefix(header, "Bearer ") { return false }\n token := strings.TrimPrefix(header, "Bearer ")\n return token != "" && !strings.ContainsAny(token, " \\t\\n")\n}\n'
TEST = 'package auth\nimport "testing"\nfunc TestTokenBoundary(t *testing.T) {\n for _, tc := range []struct{header string; valid bool}{\n {"Bearer signed-token",true},{"",false},{"Bearer ",false},{"prefix Bearer token",false},{"Bearer two tokens",false},\n } { if got:=ValidToken(tc.header); got!=tc.valid {t.Errorf("%q: got %v want %v",tc.header,got,tc.valid)} }\n}\n'

def execute(argv, cwd, env, stdin=None, show=True, check=True):
    if show:
        display = [pathlib.Path(str(argv[0])).name, *[str(value).replace(str(cwd), '.') for value in argv[1:]]]
        print('$ ' + ' '.join(display), flush=True)
    result = subprocess.run([str(value) for value in argv], cwd=cwd, env=env, input=stdin, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=90)
    if show or result.returncode:
        print(result.stdout.rstrip().replace(str(cwd), '.'), flush=True)
    if check and result.returncode:
        raise RuntimeError('command failed: ' + str(result.returncode))
    return result

def build(package, binary, bins):
    module = ROOT
    source = ROOT / package.removeprefix('./')
    if (source / 'go.mod').exists():
        module, package = (source, '.')
    target = bins / binary
    execute(['go', 'build', '-o', target, package], module, os.environ.copy(), show=False)
    return str(target)

def shim(bins, name, body):
    target = bins / name
    target.write_text('#!/usr/bin/env python3\n' + body)
    target.chmod(493)

def demo(work, env, bins):
    script = ROOT / 'extras' / NAME / 'provider.py'
    if TOOL == 'seshy':
        core = build('./cmd/sy', 'sy', bins)
        execute([core, 'new', 'token-review', '--reference', str(work)], work, env)
    elif TOOL == 'tether':
        core = build('./cmd/tether', 'tether', bins)
        build('./cmd/tsh', 'tsh', bins)
        ssh = pathlib.Path(env['HOME']) / '.ssh'
        ssh.mkdir()
        (ssh / 'config').write_text('Host dev-checkout\n  HostName dev-checkout.example.invalid\n  User developer\n')
        shim(bins, 'tailscale', 'raise SystemExit(1)\n')
    else:
        core = shutil.which('zoxide')
        execute([core, 'add', str(work)], work, env)
    def invoke(action, item=None):
        request = {'version': 'provider/v1', 'kind': 'request', 'requestId': 'token-review', 'capability': action, 'input': {'id': item} if item else {}}
        result = execute(['python3', script, core], work, env, json.dumps(request), show=False)
        frame = json.loads(result.stdout)
        if frame['status'] != 'ok':
            raise RuntimeError(frame)
        return frame['output']
    descriptor = invoke('picker.describe')
    listed = invoke('picker.list')
    print(descriptor['title'], flush=True)
    for item in listed['items']:
        print('  ' + '  '.join(segment['text'].replace(str(work), '.') for segment in item['segments']), flush=True)
    if not listed['items']:
        raise RuntimeError('picker returned no fixture item')
    plan = invoke('picker.open', listed['items'][0]['id'])
    print('Selected: ' + plan['label'], flush=True)
    print('Directory: ' + plan['cwd'].replace(str(work), '.'), flush=True)
    print('Command: ' + (' '.join(plan['command']) if plan['command'] else 'configured shell'), flush=True)
    print('Spawn plan only. No terminal or remote connection was opened.', flush=True)

def main():
    with tempfile.TemporaryDirectory(prefix='token-review-') as temporary:
        root = pathlib.Path(temporary).resolve()
        work = root / 'checkout-service'
        bins = root / 'bin'
        bins.mkdir()
        (work / 'internal/auth').mkdir(parents=True)
        (work / 'go.mod').write_text('module checkout-service\n\ngo 1.26\n')
        path = work / 'internal/auth/token.go'
        path.write_text(OLD)
        (work / 'internal/auth/token_test.go').write_text(TEST)
        env = os.environ.copy()
        for name, dirname in [('HOME', 'home'), ('XDG_CONFIG_HOME', 'config'), ('XDG_DATA_HOME', 'data'), ('XDG_STATE_HOME', 'state'), ('XDG_CACHE_HOME', 'cache'), ('XDG_RUNTIME_DIR', 'runtime')]:
            (root / dirname).mkdir()
            env[name] = str(root / dirname)
        env['PATH'] = str(bins) + os.pathsep + env['PATH']
        env['XDG_DATA_DIRS'] = str(root / 'data')
        for name in ['ORC_SESSION_ID', 'ORC_SCOPE', 'WEZTERM_PANE', 'WEZTERM_UNIX_SOCKET', 'GATE_STATE_DIR']:
            env.pop(name, None)
        execute(['git', 'init', '-b', 'main'], work, env, show=False)
        execute(['git', 'config', 'user.name', 'Review fixture'], work, env, show=False)
        execute(['git', 'config', 'user.email', 'review@example.invalid'], work, env, show=False)
        execute(['git', 'add', '.'], work, env, show=False)
        execute(['git', 'commit', '-m', 'add token parser'], work, env, show=False)
        path.write_text(NEW)
        print(META['summary'] + '\n', flush=True)
        demo(work, env, bins)
        print('\nDemo complete', flush=True)
if __name__ == '__main__':
    main()
