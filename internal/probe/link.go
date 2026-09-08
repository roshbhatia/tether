package probe

import (
	"bufio"
	"bytes"
	"context"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var (
	tailscalePong  = regexp.MustCompile(`^pong from .* via (\S+) in ([0-9.]+m?s)$`)
	icmpTime       = regexp.MustCompile(`time[=<]([0-9.]+) ?ms`)
	icmpPacketLoss = regexp.MustCompile(`([0-9.]+)% packet loss`)
)

// LinkLayer samples the path: tailscale ping when the registry knows the
// host, else ICMP ping. It is probe-only and never runs from plan.
func LinkLayer(ctx context.Context, tools Tools, hostname string, registry Registry, now time.Time) *Link {
	if tools.Run == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if registry.Peer {
		if tailscale := tools.lookPath("tailscale"); tailscale != "" {
			out, _, err := tools.Run(ctx, tailscale, "ping", "-c", "3", "--timeout", "2s", trimDot(hostname))
			if link := parseTailscalePing(out, now); link != nil {
				return link
			}
			if err != nil && len(bytes.TrimSpace(out)) == 0 {
				return nil
			}
		}
	}
	ping := tools.lookPath("ping")
	if ping == "" {
		return nil
	}
	wait := "1000"
	if runtime.GOOS == "linux" {
		wait = "1"
	}
	out, _, _ := tools.Run(ctx, ping, "-c", "3", "-W", wait, trimDot(hostname))
	return parseICMPPing(out, now)
}

func parseTailscalePing(out []byte, now time.Time) *Link {
	var rtts []float64
	var timeouts int
	direct := false
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if match := tailscalePong.FindStringSubmatch(line); match != nil {
			if duration, err := time.ParseDuration(match[2]); err == nil {
				rtts = append(rtts, float64(duration)/float64(time.Millisecond))
			}
			direct = !strings.HasPrefix(match[1], "DERP")
			continue
		}
		if strings.Contains(line, "timed out") || strings.Contains(line, "timeout") {
			timeouts++
		}
	}
	total := len(rtts) + timeouts
	if total == 0 {
		return nil
	}
	link := &Link{Loss: float64(timeouts) / float64(total), Tool: "tailscale", At: now}
	if len(rtts) > 0 {
		link.RTTMs = mean(rtts)
		link.Direct = &direct
	}
	return link
}

func parseICMPPing(out []byte, now time.Time) *Link {
	var rtts []float64
	loss := -1.0
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		if match := icmpTime.FindStringSubmatch(line); match != nil {
			if value, err := strconv.ParseFloat(match[1], 64); err == nil {
				rtts = append(rtts, value)
			}
		}
		if match := icmpPacketLoss.FindStringSubmatch(line); match != nil {
			if value, err := strconv.ParseFloat(match[1], 64); err == nil {
				loss = value / 100
			}
		}
	}
	if loss < 0 && len(rtts) == 0 {
		return nil
	}
	if loss < 0 {
		loss = 1 - float64(len(rtts))/3
	}
	return &Link{RTTMs: mean(rtts), Loss: loss, Tool: "ping", At: now}
}

func mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var sum float64
	for _, value := range values {
		sum += value
	}
	return sum / float64(len(values))
}
