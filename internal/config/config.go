package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type Account struct {
	AppID  string `json:"appid" yaml:"appid"`
	Secret string `json:"secret" yaml:"secret"`
}

type Config struct {
	WechatAppID          string
	WechatSecret         string
	WechatProxyURL       string
	WechatDefaultAccount string
	WechatAccounts       map[string]Account
	WechatAccount        string
	MD2WechatAPIKey      string
	MD2WechatBaseURL     string
	DefaultTheme         string
	DefaultBackground    string
	HTTPTimeout          int
	configFile           string
}

type fileConfig struct {
	Wechat struct {
		AppID          string             `json:"appid" yaml:"appid"`
		Secret         string             `json:"secret" yaml:"secret"`
		ProxyURL       string             `json:"proxy_url" yaml:"proxy_url"`
		DefaultAccount string             `json:"default_account" yaml:"default_account"`
		Accounts       map[string]Account `json:"accounts" yaml:"accounts"`
	} `json:"wechat" yaml:"wechat"`
	API struct {
		Key            string `json:"md2wechat_key" yaml:"md2wechat_key"`
		BaseURL        string `json:"md2wechat_base_url" yaml:"md2wechat_base_url"`
		DefaultTheme   string `json:"default_theme" yaml:"default_theme"`
		BackgroundType string `json:"background_type" yaml:"background_type"`
		HTTPTimeout    int    `json:"http_timeout" yaml:"http_timeout"`
	} `json:"api" yaml:"api"`
}

func Load(path string) (*Config, error) {
	cfg := &Config{
		MD2WechatBaseURL:  "https://www.md2wechat.cn",
		DefaultTheme:      "default",
		DefaultBackground: "none",
		HTTPTimeout:       30,
	}
	if path == "" {
		path = findConfigFile()
	}
	if path != "" {
		if err := loadFile(cfg, path); err != nil {
			return nil, err
		}
		cfg.configFile = path
	}
	loadEnv(cfg)
	if err := cfg.ResolveAccount(""); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) ResolveAccount(explicit string) error {
	name := firstNonEmpty(explicit, os.Getenv("WECHAT_ACCOUNT"), c.WechatDefaultAccount)
	if name == "" {
		if c.WechatAppID != "" || c.WechatSecret != "" {
			return nil
		}
		if len(c.WechatAccounts) == 1 {
			for accountName := range c.WechatAccounts {
				name = accountName
			}
		}
	}
	if name == "" {
		return nil
	}
	account, ok := c.WechatAccounts[name]
	if !ok {
		return fmt.Errorf("wechat account %q not found", name)
	}
	if strings.TrimSpace(account.AppID) == "" || strings.TrimSpace(account.Secret) == "" {
		return fmt.Errorf("wechat account %q is missing appid or secret", name)
	}
	c.WechatAppID = strings.TrimSpace(account.AppID)
	c.WechatSecret = strings.TrimSpace(account.Secret)
	c.WechatAccount = name
	return nil
}

func (c *Config) ValidateConversion() error {
	if strings.TrimSpace(c.MD2WechatAPIKey) == "" {
		return fmt.Errorf("MD2WECHAT_API_KEY is required")
	}
	if strings.TrimSpace(c.MD2WechatBaseURL) == "" {
		return fmt.Errorf("MD2WECHAT_BASE_URL is required")
	}
	return nil
}

func (c *Config) ValidateWeChat() error {
	if strings.TrimSpace(c.WechatAppID) == "" {
		return fmt.Errorf("WECHAT_APPID is required")
	}
	if strings.TrimSpace(c.WechatSecret) == "" {
		return fmt.Errorf("WECHAT_SECRET is required")
	}
	return nil
}

func (c *Config) PublicView() map[string]any {
	accounts := make([]string, 0, len(c.WechatAccounts))
	for name := range c.WechatAccounts {
		accounts = append(accounts, name)
	}
	sort.Strings(accounts)
	return map[string]any{
		"config_file": c.configFile, "md2wechat_base_url": c.MD2WechatBaseURL,
		"default_theme": c.DefaultTheme, "default_background_type": c.DefaultBackground,
		"http_timeout": c.HTTPTimeout, "wechat_account": c.WechatAccount,
		"wechat_accounts": accounts, "conversion_configured": c.MD2WechatAPIKey != "",
		"wechat_configured": c.WechatAppID != "" && c.WechatSecret != "",
	}
}

func loadFile(cfg *Config, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	var file fileConfig
	if strings.EqualFold(filepath.Ext(path), ".json") {
		err = json.Unmarshal(data, &file)
	} else {
		err = yaml.Unmarshal(data, &file)
	}
	if err != nil {
		return fmt.Errorf("parse config: %w", err)
	}
	cfg.WechatAppID = file.Wechat.AppID
	cfg.WechatSecret = file.Wechat.Secret
	cfg.WechatProxyURL = file.Wechat.ProxyURL
	cfg.WechatDefaultAccount = file.Wechat.DefaultAccount
	cfg.WechatAccounts = file.Wechat.Accounts
	cfg.MD2WechatAPIKey = file.API.Key
	cfg.MD2WechatBaseURL = firstNonEmpty(file.API.BaseURL, cfg.MD2WechatBaseURL)
	cfg.DefaultTheme = firstNonEmpty(file.API.DefaultTheme, cfg.DefaultTheme)
	cfg.DefaultBackground = firstNonEmpty(file.API.BackgroundType, cfg.DefaultBackground)
	if file.API.HTTPTimeout > 0 {
		cfg.HTTPTimeout = file.API.HTTPTimeout
	}
	return nil
}

func loadEnv(cfg *Config) {
	setIfPresent(&cfg.WechatAppID, "WECHAT_APPID")
	setIfPresent(&cfg.WechatSecret, "WECHAT_SECRET")
	setIfPresent(&cfg.WechatProxyURL, "WECHAT_PROXY_URL")
	setIfPresent(&cfg.MD2WechatAPIKey, "MD2WECHAT_API_KEY")
	setIfPresent(&cfg.MD2WechatBaseURL, "MD2WECHAT_BASE_URL")
	setIfPresent(&cfg.DefaultTheme, "DEFAULT_THEME")
	setIfPresent(&cfg.DefaultBackground, "DEFAULT_BACKGROUND_TYPE")
}

func setIfPresent(target *string, key string) {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		*target = value
	}
}

func findConfigFile() string {
	home, _ := os.UserHomeDir()
	paths := []string{
		filepath.Join(home, ".config", "md2wechat", "config.yaml"),
		filepath.Join(home, ".md2wechat.yaml"),
		"md2wechat.yaml", "md2wechat.yml", "md2wechat.json",
	}
	for _, path := range paths {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
