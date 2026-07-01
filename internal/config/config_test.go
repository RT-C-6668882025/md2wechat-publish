package config

import "testing"

func TestPublishingConfigContainsOnlyPublishingFields(t *testing.T) {
	cfg := &Config{
		MD2WechatAPIKey:  "convert-key",
		MD2WechatBaseURL: "https://www.md2wechat.cn",
		WechatAppID:      "appid",
		WechatSecret:     "secret",
	}
	if err := cfg.ValidateConversion(); err != nil {
		t.Fatal(err)
	}
	if err := cfg.ValidateWeChat(); err != nil {
		t.Fatal(err)
	}
}

func TestEnvironmentOverridesPublishingConfig(t *testing.T) {
	t.Setenv("MD2WECHAT_BASE_URL", "https://convert.example.com")
	t.Setenv("WECHAT_APPID", "env-appid")
	t.Setenv("WECHAT_SECRET", "env-secret")
	cfg := &Config{MD2WechatBaseURL: "https://default.example.com"}
	loadEnv(cfg)
	if cfg.MD2WechatBaseURL != "https://convert.example.com" || cfg.WechatAppID != "env-appid" {
		t.Fatalf("environment overrides not applied: %#v", cfg.PublicView())
	}
}
