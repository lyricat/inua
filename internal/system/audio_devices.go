package system

import (
	"context"
	"encoding/json"
	"os/exec"
	"strconv"
	"strings"
)

// AudioDevice is an output or an input PipeWire has: its node id (wpctl's),
// its node name and its description, "DJI MIC MINI Analog Stereo".
type AudioDevice struct {
	ID          int
	Name, Title string
}

// audioDevices lists PipeWire's outputs and inputs (`pw-dump`), and the
// node names of the defaults, which sound plays to and dictation records
// from.
func audioDevices(ctx context.Context) (devs audioGraph, err error) {
	ctx, cancel := context.WithTimeout(ctx, callLimit)
	defer cancel()
	out, err := exec.CommandContext(ctx, "pw-dump", "-N").Output()
	if err != nil {
		return audioGraph{}, err
	}
	return parseAudioDevices(out)
}

// audioGraph is what parseAudioDevices finds.
type audioGraph struct {
	outputs, inputs []AudioDevice
	output, input   string // the defaults' node names
}

// parseAudioDevices reads pw-dump's objects: the Audio/Sink and
// Audio/Source nodes, and the "default" metadata's default.audio.sink and
// default.audio.source, the defaults' node names.
func parseAudioDevices(dump []byte) (audioGraph, error) {
	var objs []struct {
		ID    int    `json:"id"`
		Type  string `json:"type"`
		Props struct {
			Name string `json:"metadata.name"`
		} `json:"props"`
		Info *struct {
			Props map[string]any `json:"props"`
		} `json:"info"`
		Metadata []struct {
			Key   string          `json:"key"`
			Value json.RawMessage `json:"value"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(dump, &objs); err != nil {
		return audioGraph{}, err
	}
	var g audioGraph
	for _, o := range objs {
		if o.Type == "PipeWire:Interface:Metadata" && o.Props.Name == "default" {
			for _, m := range o.Metadata {
				var v struct{ Name string }
				if json.Unmarshal(m.Value, &v) != nil {
					continue
				}
				switch m.Key {
				case "default.audio.sink":
					g.output = v.Name
				case "default.audio.source":
					g.input = v.Name
				}
			}
		}
		if o.Info == nil {
			continue
		}
		p := o.Info.Props
		name, _ := p["node.name"].(string)
		title, _ := p["node.description"].(string)
		d := AudioDevice{ID: o.ID, Name: name, Title: strings.TrimSpace(title)}
		switch class, _ := p["media.class"].(string); class {
		case "Audio/Sink":
			g.outputs = append(g.outputs, d)
		case "Audio/Source":
			g.inputs = append(g.inputs, d)
		}
	}
	return g, nil
}

// SetDefaultDevice makes an output or an input PipeWire's default.
func SetDefaultDevice(ctx context.Context, id int) error {
	return runStep(ctx, "wpctl", "set-default", strconv.Itoa(id))
}
