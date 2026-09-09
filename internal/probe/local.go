package probe

import (
	"bufio"
	"bytes"
	"context"
	"strconv"
	"strings"
)

// LocalLayer answers command -v for the local hop tools and resolves the
// ssh alias with ssh -G, which opens no connection.
func LocalLayer(ctx context.Context, tools Tools, host string) Local {
	local := Local{
		Mosh:     tools.lookPath("mosh"),
		Wezterm:  tools.lookPath("wezterm"),
		SSH:      tools.lookPath("ssh"),
		Hostname: hostPart(host),
	}
	if local.SSH == "" || tools.Run == nil {
		return local
	}
	out, _, err := tools.Run(ctx, local.SSH, "-G", host)
	if err != nil {
		return local
	}
	hostname, user, port := parseSSHConfig(out)
	if hostname != "" {
		local.Hostname = hostname
	}
	local.User = user
	local.Port = port
	return local
}

// hostPart strips a user@ prefix.
func hostPart(target string) string {
	if _, host, ok := strings.Cut(target, "@"); ok {
		return host
	}
	return target
}

func parseSSHConfig(out []byte) (hostname, user string, port int) {
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		key, value, ok := strings.Cut(strings.TrimSpace(scanner.Text()), " ")
		if !ok {
			continue
		}
		switch key {
		case "hostname":
			hostname = value
		case "user":
			user = value
		case "port":
			port, _ = strconv.Atoi(value)
		}
	}
	return hostname, user, port
}
