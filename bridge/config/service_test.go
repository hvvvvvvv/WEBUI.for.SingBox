package config

import (
	"testing"

	"guiforcores/bridge/storage"
	kernelv1 "guiforcores/gen/kernel/v1"
	profilev1 "guiforcores/gen/profile/v1"
)

func TestGenerateOptionsPreserveMixinAndScriptBehavior(t *testing.T) {
	profile := &profilev1.Profile{
		Mixin: &profilev1.Mixin{
			Priority: profilev1.MixinPriority_MIXIN_PRIORITY_MIXIN,
			Config:   `{"step":"mixin"}`,
		},
		Script: &profilev1.Script{
			Code: `function onGenerate(config) {
				config.step = (config.step || "base") + "-script";
				return config;
			}`,
		},
	}
	for _, test := range []struct {
		name    string
		options *kernelv1.GenerateConfigOptions
		step    any
	}{
		{name: "omitted options enable only mixin", step: "mixin"},
		{name: "empty options disable both", options: &kernelv1.GenerateConfigOptions{}},
		{
			name:    "mixin only",
			options: &kernelv1.GenerateConfigOptions{EnableMixinProcessing: true},
			step:    "mixin",
		},
		{
			name:    "script only",
			options: &kernelv1.GenerateConfigOptions{EnableScriptProcessing: true},
			step:    "base-script",
		},
		{
			name: "stable mixin precedes script",
			options: &kernelv1.GenerateConfigOptions{
				EnableMixinProcessing: true, EnableScriptProcessing: true,
			},
			step: "mixin-script",
		},
		{
			name: "alpha mixin precedes script",
			options: &kernelv1.GenerateConfigOptions{
				EnableAlphaConfigAdaptation: true,
				EnableMixinProcessing:       true, EnableScriptProcessing: true,
			},
			step: "mixin-script",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := NewService(storage.NewPaths(t.TempDir()), "test")
			generated, err := service.Generate(profile, test.options)
			if err != nil {
				t.Fatal(err)
			}
			if got := generated["step"]; got != test.step {
				t.Fatalf("generated step = %v, want %v", got, test.step)
			}
		})
	}
}
