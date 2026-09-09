package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type request struct {
	Version    string `json:"version"`
	Kind       string `json:"kind"`
	RequestID  string `json:"requestId"`
	Capability string `json:"capability"`
	Input      struct {
		ID string `json:"id"`
	} `json:"input"`
}
type response struct {
	Version   string `json:"version"`
	Kind      string `json:"kind"`
	RequestID string `json:"requestId"`
	Status    string `json:"status"`
	Output    any    `json:"output,omitempty"`
	Message   string `json:"message,omitempty"`
}
type segment struct {
	Text string `json:"text"`
	Role string `json:"role"`
}
type item struct {
	ID       string    `json:"id"`
	Search   string    `json:"search"`
	Segments []segment `json:"segments"`
}
type plan struct {
	Kind        string            `json:"kind"`
	Label       string            `json:"label"`
	Cwd         string            `json:"cwd"`
	Command     []string          `json:"command"`
	Environment map[string]string `json:"environment"`
}
type adapter struct {
	core    string
	timeout time.Duration
}

func (a adapter) run(output any, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), a.timeout)
	defer cancel()
	data, err := exec.CommandContext(ctx, a.core, args...).Output()
	if ctx.Err() != nil {
		return fmt.Errorf("%s: %w", a.core, ctx.Err())
	}
	if err != nil {
		return fmt.Errorf("%s: %w", a.core, err)
	}
	if output == nil {
		return nil
	}
	if err := json.Unmarshal(data, output); err != nil {
		return fmt.Errorf("%s returned invalid JSON: %w", a.core, err)
	}
	return nil
}
func serve(in io.Reader, out io.Writer, a adapter) error {
	r := request{RequestID: "invalid"}
	err := json.NewDecoder(in).Decode(&r)
	var output any
	if err == nil {
		if r.Version != "provider/v1" || r.Kind != "request" {
			err = errors.New("unsupported request")
		} else {
			output, err = a.handle(r)
		}
	}
	result := response{Version: "provider/v1", Kind: "result", RequestID: r.RequestID, Status: "ok", Output: output}
	if err != nil {
		result.Status = "error"
		result.Message = err.Error()
	}
	return json.NewEncoder(out).Encode(result)
}
func main() {
	core := defaultCore
	if len(os.Args) > 1 {
		core = os.Args[1]
	}
	if err := serve(os.Stdin, os.Stdout, adapter{core: core, timeout: 4 * time.Second}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

const defaultCore = "tether"

type host struct {
	Name     string `json:"name"`
	Hostname string `json:"hostname"`
	Target   string `json:"target"`
	Peer     *struct {
		Online bool `json:"online"`
	} `json:"peer"`
}

func (a adapter) hosts() ([]host, error) {
	var result struct {
		Hosts []host `json:"hosts"`
	}
	err := a.run(&result, "hosts", "--json")
	return result.Hosts, err
}
func (a adapter) connector() string {
	if filepath.Base(a.core) != a.core {
		return filepath.Join(filepath.Dir(a.core), "tsh")
	}
	return "tsh"
}
func (a adapter) handle(r request) (any, error) {
	switch r.Capability {
	case "provider.validate":
		if err := a.run(nil, "--help"); err != nil {
			return nil, err
		}
		if _, err := exec.LookPath(a.connector()); err != nil {
			return nil, err
		}
		return map[string]bool{"ok": true}, nil
	case "picker.describe":
		return map[string]string{"title": "Hosts", "icon": "md_server"}, nil
	case "picker.list":
		rows, err := a.hosts()
		if err != nil {
			return nil, err
		}
		items := make([]item, 0, len(rows))
		for _, row := range rows {
			state := "unknown"
			if row.Peer != nil {
				state = "offline"
				if row.Peer.Online {
					state = "online"
				}
			}
			items = append(items, item{row.Name, row.Name + " " + row.Hostname, []segment{{row.Name, "name"}, {row.Hostname, "path"}, {state, "detail"}}})
		}
		return map[string]any{"items": items}, nil
	case "picker.open":
		rows, err := a.hosts()
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			if row.Name != r.Input.ID {
				continue
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return nil, err
			}
			target := row.Target
			if target == "" {
				target = row.Name
			}
			return plan{"spawn", row.Name, home, []string{a.connector(), target}, map[string]string{}}, nil
		}
		return nil, errors.New("host no longer exists")
	default:
		return nil, errors.New("unsupported capability")
	}
}
