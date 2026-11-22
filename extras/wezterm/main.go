package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
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
	Cwd         string            `json:"cwd,omitempty"`
	Domain      string            `json:"domain,omitempty"`
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
		var exit *exec.ExitError
		if errors.As(err, &exit) && len(exit.Stderr) > 0 {
			return errors.New(strings.TrimSpace(string(exit.Stderr)))
		}
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
	if err := serve(os.Stdin, os.Stdout, adapter{core: core, timeout: 15 * time.Second}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

const defaultCore = "tether"

type host struct {
	Name      string   `json:"name"`
	Hostname  string   `json:"hostname"`
	Addresses []string `json:"addresses"`
	Aliases   []string `json:"aliases"`
	Peer      *struct {
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
func (a adapter) open(name string) (plan, error) {
	var selected struct {
		Hop *struct {
			Kind string `json:"kind"`
			Ref  string `json:"ref"`
		} `json:"hop"`
		Plan *struct {
			Command     []string          `json:"command"`
			Environment map[string]string `json:"environment"`
		} `json:"plan"`
	}
	err := a.run(&selected, "plan", "--host", name, "--resolve", "--mode", "native", "--native-prefix", "ssh:")
	if err != nil {
		return plan{}, err
	}
	if selected.Hop == nil {
		return plan{}, errors.New("no usable transport")
	}
	if selected.Hop.Kind == "native" && selected.Hop.Ref != "" {
		return plan{Kind: "attach", Label: name, Domain: selected.Hop.Ref}, nil
	}
	if selected.Hop.Kind != "local" || selected.Plan == nil || len(selected.Plan.Command) == 0 {
		return plan{}, errors.New("unrecognized transport plan")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return plan{}, err
	}
	environment := selected.Plan.Environment
	if environment == nil {
		environment = map[string]string{}
	}
	return plan{Kind: "spawn", Label: name, Cwd: home, Command: selected.Plan.Command, Environment: environment}, nil
}
func (a adapter) handle(r request) (any, error) {
	switch r.Capability {
	case "provider.validate":
		if err := a.run(nil, "--help"); err != nil {
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
			if len(row.Addresses) > 1 {
				state += " · alternate address"
			}
			search := strings.Join(append(append([]string{row.Name, row.Hostname}, row.Addresses...), row.Aliases...), " ")
			items = append(items, item{row.Name, search, []segment{{row.Name, "name"}, {row.Hostname, "path"}, {state, "detail"}}})
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
			return a.open(row.Name)
		}
		return nil, errors.New("host no longer exists")
	default:
		return nil, errors.New("unsupported capability")
	}
}
