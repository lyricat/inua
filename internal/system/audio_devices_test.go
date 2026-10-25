package system

import "testing"

// PipeWire's outputs and inputs, and the defaults.
func TestParseAudioDevices(t *testing.T) {
	dump := `[
 {"id": 30, "type": "PipeWire:Interface:Node", "info": {"props": {"media.class": "Audio/Sink", "node.name": "out", "node.description": "Speakers"}}},
 {"id": 59, "type": "PipeWire:Interface:Node", "info": {"props": {"media.class": "Audio/Source", "node.name": "alsa_input.pci", "node.description": "Built-in Microphone"}}},
 {"id": 77, "type": "PipeWire:Interface:Node", "info": {"props": {"media.class": "Audio/Source", "node.name": "alsa_input.usb", "node.description": "DJI MIC MINI "}}},
 {"id": 81, "type": "PipeWire:Interface:Node", "info": {"props": {"media.class": "Stream/Output/Audio", "node.name": "firefox"}}},
 {"id": 40, "type": "PipeWire:Interface:Metadata", "props": {"metadata.name": "default"}, "metadata": [
  {"subject": 0, "key": "default.audio.sink", "value": {"name": "out"}},
  {"subject": 0, "key": "default.audio.source", "value": {"name": "alsa_input.usb"}}]}
]`
	g, err := parseAudioDevices([]byte(dump))
	if err != nil || g.output != "out" || g.input != "alsa_input.usb" ||
		len(g.outputs) != 1 || g.outputs[0] != (AudioDevice{ID: 30, Name: "out", Title: "Speakers"}) ||
		len(g.inputs) != 2 || g.inputs[1] != (AudioDevice{ID: 77, Name: "alsa_input.usb", Title: "DJI MIC MINI"}) {
		t.Errorf("%+v %v", g, err)
	}
}
