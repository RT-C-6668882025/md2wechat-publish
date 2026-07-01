package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	stdhtml "html"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	cfgpkg "github.com/geekjourneyx/md2wechat-skill/projects/md2wechat-publish/internal/config"
	wechatapi "github.com/geekjourneyx/md2wechat-skill/projects/md2wechat-publish/internal/wechat"
	"github.com/silenceper/wechat/v2/officialaccount/draft"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

var version = "dev"
var configPath string

type cliResponse struct {
	Success       bool   `json:"success"`
	SchemaVersion string `json:"schema_version"`
	Data          any    `json:"data,omitempty"`
	Error         string `json:"error,omitempty"`
}

type articleMetadata struct {
	Title  string `json:"title"`
	Author string `json:"author,omitempty"`
	Digest string `json:"digest,omitempty"`
}

type articleDocument struct {
	Metadata articleMetadata
	Body     string
}

type frontMatter struct {
	Title       string `yaml:"title"`
	Author      string `yaml:"author"`
	Digest      string `yaml:"digest"`
	Summary     string `yaml:"summary"`
	Description string `yaml:"description"`
}

type imageRef struct {
	Index       int    `json:"index"`
	Source      string `json:"source"`
	Local       bool   `json:"local"`
	Placeholder string `json:"placeholder"`
	PublicURL   string `json:"public_url,omitempty"`
	MediaID     string `json:"media_id,omitempty"`
}

type articleOptions struct {
	theme         string
	fontSize      string
	background    string
	apiKey        string
	output        string
	title         string
	author        string
	digest        string
	cover         string
	coverMediaID  string
	wechatAccount string
}

type conversionAPIResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		HTML string `json:"html"`
	} `json:"data"`
}

func main() {
	if err := newRootCommand().Execute(); err != nil {
		writeError(err)
		os.Exit(1)
	}
}

func newRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "md2wechat-publish",
		Short:         "Convert prepared Markdown and publish it to WeChat",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&configPath, "config", "", "Explicit YAML or JSON config file")
	root.AddCommand(
		newInspectCommand(),
		newConvertCommand(),
		newUploadCommand(),
		newDraftCommand(),
		newConfigCommand(),
		&cobra.Command{
			Use:   "version",
			Short: "Show version",
			Run: func(cmd *cobra.Command, args []string) {
				writeJSON(map[string]any{"version": version})
			},
		},
	)
	return root
}

func newInspectCommand() *cobra.Command {
	var opts articleOptions
	var draftRequested bool
	cmd := &cobra.Command{
		Use:   "inspect <article.md>",
		Short: "Inspect conversion and draft readiness without side effects",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, markdown, err := loadArticle(args[0], opts.wechatAccount)
			if err != nil {
				return err
			}
			doc := parseArticle(markdown)
			metadata := resolveMetadata(doc.Metadata, opts)
			images, imageErr := parseImages(doc.Body)
			checks := make([]map[string]any, 0)
			conversionReady := cfg.ValidateConversion() == nil
			draftReady := conversionReady && cfg.ValidateWeChat() == nil
			if err := cfg.ValidateConversion(); err != nil {
				checks = append(checks, check("error", "CONVERSION_CONFIG_INVALID", err.Error()))
			}
			if err := cfg.ValidateWeChat(); err != nil {
				checks = append(checks, check("error", "WECHAT_CONFIG_INVALID", err.Error()))
			}
			if err := validateMetadata(metadata); err != nil {
				draftReady = false
				checks = append(checks, check("error", "METADATA_INVALID", err.Error()))
			}
			if imageErr != nil {
				conversionReady = false
				draftReady = false
				checks = append(checks, check("error", "IMAGE_REFERENCE_INVALID", imageErr.Error()))
			}
			for _, ref := range images {
				if ref.Local {
					path := resolveLocalPath(filepath.Dir(args[0]), ref.Source)
					if info, err := os.Stat(path); err != nil || info.IsDir() {
						draftReady = false
						checks = append(checks, check("error", "LOCAL_IMAGE_MISSING", path))
					}
				}
			}
			if draftRequested && opts.cover == "" && opts.coverMediaID == "" {
				draftReady = false
				checks = append(checks, check("error", "COVER_REQUIRED", "draft creation requires --cover or --cover-media-id"))
			}
			if opts.cover != "" {
				if info, err := os.Stat(opts.cover); err != nil || info.IsDir() {
					draftReady = false
					checks = append(checks, check("error", "COVER_MISSING", opts.cover))
				}
			}
			if opts.cover != "" && opts.coverMediaID != "" {
				draftReady = false
				checks = append(checks, check("error", "COVER_CONFLICT", "--cover and --cover-media-id are mutually exclusive"))
			}
			writeJSON(map[string]any{
				"metadata": metadata,
				"images":   images,
				"checks":   checks,
				"readiness": map[string]any{
					"targets": map[string]string{
						"convert": readiness(conversionReady),
						"draft":   readiness(draftReady),
					},
				},
			})
			return nil
		},
	}
	addArticleFlags(cmd, &opts)
	cmd.Flags().BoolVar(&draftRequested, "draft", false, "Check draft readiness")
	cmd.Flags().StringVar(&opts.coverMediaID, "cover-media-id", "", "Existing WeChat cover media ID")
	cmd.Flags().StringVar(&opts.wechatAccount, "wechat-account", "", "Named WeChat account")
	return cmd
}

func newConvertCommand() *cobra.Command {
	var opts articleOptions
	cmd := &cobra.Command{
		Use:   "convert <article.md>",
		Short: "Convert prepared Markdown to WeChat HTML",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, markdown, err := loadArticle(args[0], "")
			if err != nil {
				return err
			}
			if opts.apiKey != "" {
				cfg.MD2WechatAPIKey = opts.apiKey
			}
			doc := parseArticle(markdown)
			metadata := resolveMetadata(doc.Metadata, opts)
			html, images, err := convertMarkdown(cmd, cfg, doc.Body, opts)
			if err != nil {
				return err
			}
			if opts.output != "" {
				if err := os.WriteFile(opts.output, []byte(html), 0o644); err != nil {
					return fmt.Errorf("write HTML: %w", err)
				}
				writeJSON(map[string]any{"status": "completed", "output": opts.output, "metadata": metadata, "images": images})
				return nil
			}
			writeJSON(map[string]any{"status": "completed", "html": html, "metadata": metadata, "images": images})
			return nil
		},
	}
	addArticleFlags(cmd, &opts)
	cmd.Flags().StringVarP(&opts.output, "output", "o", "", "HTML output path")
	cmd.Flags().StringVar(&opts.apiKey, "api-key", "", "Conversion API key override")
	return cmd
}

func newUploadCommand() *cobra.Command {
	var account string
	cmd := &cobra.Command{
		Use:   "upload <image>",
		Short: "Upload an existing local image to WeChat",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(account)
			if err != nil {
				return err
			}
			if err := cfg.ValidateWeChat(); err != nil {
				return err
			}
			result, err := newWeChatService(cfg).UploadMaterial(args[0])
			if err != nil {
				return err
			}
			writeJSON(result)
			return nil
		},
	}
	cmd.Flags().StringVar(&account, "wechat-account", "", "Named WeChat account")
	return cmd
}

func newDraftCommand() *cobra.Command {
	var opts articleOptions
	cmd := &cobra.Command{
		Use:   "draft <article.md>",
		Short: "Convert, upload assets, and create a WeChat draft",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, markdown, err := loadArticle(args[0], opts.wechatAccount)
			if err != nil {
				return err
			}
			if opts.apiKey != "" {
				cfg.MD2WechatAPIKey = opts.apiKey
			}
			if err := cfg.ValidateWeChat(); err != nil {
				return err
			}
			if opts.cover != "" && opts.coverMediaID != "" {
				return fmt.Errorf("--cover and --cover-media-id are mutually exclusive")
			}
			if opts.cover == "" && opts.coverMediaID == "" {
				return fmt.Errorf("draft creation requires --cover or --cover-media-id")
			}
			doc := parseArticle(markdown)
			metadata := resolveMetadata(doc.Metadata, opts)
			if err := validateMetadata(metadata); err != nil {
				return err
			}
			if opts.cover != "" {
				if info, err := os.Stat(opts.cover); err != nil || info.IsDir() {
					return fmt.Errorf("cover file is not readable: %s", opts.cover)
				}
			}
			html, images, err := convertMarkdown(cmd, cfg, doc.Body, opts)
			if err != nil {
				return err
			}
			service := newWeChatService(cfg)
			html, images, err = uploadImages(service, html, images, filepath.Dir(args[0]))
			if err != nil {
				return err
			}
			coverID := opts.coverMediaID
			if coverID == "" {
				result, err := service.UploadMaterial(opts.cover)
				if err != nil {
					return fmt.Errorf("upload cover: %w", err)
				}
				coverID = result.MediaID
			}
			digest := metadata.Digest
			if digest == "" {
				digest = digestFromHTML(html, 120)
			}
			result, err := service.CreateDraft([]*draft.Article{{
				Title: metadata.Title, Author: metadata.Author, Digest: digest,
				Content: html, ThumbMediaID: coverID, ShowCoverPic: 1,
			}})
			if err != nil {
				return wechatapi.ExplainDraftError(err)
			}
			writeJSON(map[string]any{
				"status": "completed", "media_id": result.MediaID, "draft_url": result.DraftURL,
				"cover_media_id": coverID, "images": images,
			})
			return nil
		},
	}
	addArticleFlags(cmd, &opts)
	cmd.Flags().StringVar(&opts.apiKey, "api-key", "", "Conversion API key override")
	cmd.Flags().StringVar(&opts.coverMediaID, "cover-media-id", "", "Existing WeChat cover media ID")
	cmd.Flags().StringVar(&opts.wechatAccount, "wechat-account", "", "Named WeChat account")
	return cmd
}

func newConfigCommand() *cobra.Command {
	root := &cobra.Command{Use: "config", Short: "Inspect publishing configuration"}
	root.AddCommand(
		&cobra.Command{
			Use: "show", Short: "Show non-secret effective configuration",
			RunE: func(cmd *cobra.Command, args []string) error {
				cfg, err := loadConfig("")
				if err != nil {
					return err
				}
				writeJSON(cfg.PublicView())
				return nil
			},
		},
		&cobra.Command{
			Use: "validate", Short: "Validate conversion and WeChat readiness",
			RunE: func(cmd *cobra.Command, args []string) error {
				cfg, err := loadConfig("")
				if err != nil {
					return err
				}
				conversionErr := cfg.ValidateConversion()
				wechatErr := cfg.ValidateWeChat()
				writeJSON(map[string]any{
					"conversion_ready": conversionErr == nil,
					"wechat_ready":     wechatErr == nil,
					"conversion_error": errorString(conversionErr),
					"wechat_error":     errorString(wechatErr),
				})
				return nil
			},
		},
	)
	return root
}

func addArticleFlags(cmd *cobra.Command, opts *articleOptions) {
	cmd.Flags().StringVar(&opts.theme, "theme", "", "Conversion theme")
	cmd.Flags().StringVar(&opts.fontSize, "font-size", "medium", "small, medium, or large")
	cmd.Flags().StringVar(&opts.background, "background", "", "default, grid, or none")
	cmd.Flags().StringVar(&opts.title, "title", "", "Title override")
	cmd.Flags().StringVar(&opts.author, "author", "", "Author override")
	cmd.Flags().StringVar(&opts.digest, "digest", "", "Digest override")
	cmd.Flags().StringVar(&opts.cover, "cover", "", "Local cover image")
}

func loadArticle(path, account string) (*cfgpkg.Config, string, error) {
	cfg, err := loadConfig(account)
	if err != nil {
		return nil, "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("read article: %w", err)
	}
	return cfg, string(data), nil
}

func loadConfig(account string) (*cfgpkg.Config, error) {
	cfg, err := cfgpkg.Load(configPath)
	if err != nil {
		return nil, err
	}
	if account != "" {
		if err := cfg.ResolveAccount(account); err != nil {
			return nil, err
		}
	}
	return cfg, nil
}

func newWeChatService(cfg *cfgpkg.Config) *wechatapi.Service {
	return wechatapi.NewService(cfg, zap.NewNop())
}

func convertMarkdown(cmd *cobra.Command, cfg *cfgpkg.Config, markdown string, opts articleOptions) (string, []imageRef, error) {
	if err := cfg.ValidateConversion(); err != nil {
		return "", nil, err
	}
	images, err := parseImages(markdown)
	if err != nil {
		return "", nil, err
	}
	baseURL := strings.TrimRight(cfg.MD2WechatBaseURL, "/")
	if !strings.HasSuffix(baseURL, "/api/convert") {
		baseURL += "/api/convert"
	}
	requestBody := map[string]any{
		"markdown":       markdown,
		"theme":          firstNonEmpty(opts.theme, cfg.DefaultTheme),
		"fontSize":       opts.fontSize,
		"backgroundType": firstNonEmpty(opts.background, cfg.DefaultBackground),
	}
	payload, err := json.Marshal(requestBody)
	if err != nil {
		return "", nil, err
	}
	req, err := http.NewRequestWithContext(cmd.Context(), http.MethodPost, baseURL, bytes.NewReader(payload))
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", cfg.MD2WechatAPIKey)
	client := &http.Client{Timeout: time.Duration(cfg.HTTPTimeout) * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("conversion request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return "", nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", nil, fmt.Errorf("conversion API returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var result conversionAPIResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", nil, fmt.Errorf("parse conversion response: %w", err)
	}
	if result.Code != 0 {
		return "", nil, fmt.Errorf("conversion API error %d: %s", result.Code, result.Msg)
	}
	if strings.TrimSpace(result.Data.HTML) == "" {
		return "", nil, fmt.Errorf("conversion API returned empty HTML")
	}
	return insertImagePlaceholders(result.Data.HTML, images), images, nil
}

func uploadImages(service *wechatapi.Service, html string, images []imageRef, markdownDir string) (string, []imageRef, error) {
	for i := range images {
		path := images[i].Source
		removeAfter := false
		if images[i].Local {
			path = resolveLocalPath(markdownDir, path)
		} else {
			var err error
			path, err = wechatapi.DownloadFile(path)
			if err != nil {
				return "", nil, fmt.Errorf("download image %d: %w", i, err)
			}
			removeAfter = true
		}
		result, err := service.UploadMaterial(path)
		if removeAfter {
			_ = os.Remove(path)
		}
		if err != nil {
			return "", nil, fmt.Errorf("upload image %d: %w", i, err)
		}
		images[i].MediaID = result.MediaID
		images[i].PublicURL = result.WechatURL
	}
	return replaceImagePlaceholders(html, images), images, nil
}

func parseArticle(markdown string) articleDocument {
	document := articleDocument{Body: normalizeNewlines(markdown)}
	lines := strings.Split(document.Body, "\n")
	if len(lines) >= 3 && strings.TrimSpace(lines[0]) == "---" {
		for i := 1; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) != "---" {
				continue
			}
			var fm frontMatter
			if yaml.Unmarshal([]byte(strings.Join(lines[1:i], "\n")), &fm) == nil {
				document.Metadata = articleMetadata{
					Title: strings.TrimSpace(fm.Title), Author: strings.TrimSpace(fm.Author),
					Digest: firstNonEmpty(fm.Digest, fm.Summary, fm.Description),
				}
				document.Body = strings.Join(lines[i+1:], "\n")
			}
			break
		}
	}
	if document.Metadata.Title == "" {
		document.Metadata.Title = markdownTitle(document.Body)
	}
	return document
}

var imagePattern = regexp.MustCompile("!\\[[^\\]]*\\]\\((<[^>]+>|[^)\\s]+)(?:\\s+(?:\"[^\"]*\"|'[^']*'|\\([^)]*\\)))?\\)")

func parseImages(markdown string) ([]imageRef, error) {
	matches := imagePattern.FindAllStringSubmatch(markdown, -1)
	images := make([]imageRef, 0, len(matches))
	for _, match := range matches {
		source := strings.Trim(strings.TrimSpace(match[1]), "<>")
		if strings.HasPrefix(source, "__generate:") {
			return nil, fmt.Errorf("unresolved generated-image placeholder: %s", source)
		}
		if strings.HasPrefix(source, "http://") {
			return nil, fmt.Errorf("insecure remote image URL is not allowed: %s", source)
		}
		local := !strings.HasPrefix(source, "https://")
		images = append(images, imageRef{
			Index: len(images), Source: source, Local: local,
			Placeholder: fmt.Sprintf("<!-- IMG:%d -->", len(images)),
		})
	}
	return images, nil
}

func insertImagePlaceholders(html string, images []imageRef) string {
	result := html
	inserted := make(map[int]bool)
	for _, image := range images {
		for _, source := range []string{image.Source, stdhtml.EscapeString(image.Source)} {
			doubleQuoted := regexp.MustCompile("(?i)<img[^>]*src=\"" + regexp.QuoteMeta(source) + "\"[^>]*>")
			singleQuoted := regexp.MustCompile("(?i)<img[^>]*src='" + regexp.QuoteMeta(source) + "'[^>]*>")
			if doubleQuoted.MatchString(result) || singleQuoted.MatchString(result) {
				inserted[image.Index] = true
			}
			result = doubleQuoted.ReplaceAllString(result, image.Placeholder)
			result = singleQuoted.ReplaceAllString(result, image.Placeholder)
		}
	}
	tagPattern := regexp.MustCompile("(?i)<img\\b[^>]*>")
	for _, image := range images {
		if inserted[image.Index] {
			continue
		}
		result = tagPattern.ReplaceAllStringFunc(result, func(tag string) string {
			if inserted[image.Index] {
				return tag
			}
			inserted[image.Index] = true
			return image.Placeholder
		})
	}
	return result
}

func replaceImagePlaceholders(html string, images []imageRef) string {
	result := html
	for _, image := range images {
		if image.PublicURL == "" {
			continue
		}
		tag := "<img src=\"" + image.PublicURL + "\" style=\"max-width:100%;height:auto;display:block;margin:20px auto;\" />"
		result = strings.ReplaceAll(result, image.Placeholder, tag)
		result = strings.ReplaceAll(result, "src=\""+image.Source+"\"", "src=\""+image.PublicURL+"\"")
		result = strings.ReplaceAll(result, "src='"+image.Source+"'", "src='"+image.PublicURL+"'")
	}
	return result
}

func resolveMetadata(base articleMetadata, opts articleOptions) articleMetadata {
	return articleMetadata{
		Title:  firstNonEmpty(opts.title, base.Title),
		Author: firstNonEmpty(opts.author, base.Author),
		Digest: firstNonEmpty(opts.digest, base.Digest),
	}
}

func validateMetadata(metadata articleMetadata) error {
	if strings.TrimSpace(metadata.Title) == "" {
		return fmt.Errorf("article title is required")
	}
	if utf8.RuneCountInString(metadata.Title) > 32 {
		return fmt.Errorf("article title exceeds 32 characters")
	}
	if utf8.RuneCountInString(metadata.Author) > 16 {
		return fmt.Errorf("author exceeds 16 characters")
	}
	if utf8.RuneCountInString(metadata.Digest) > 128 {
		return fmt.Errorf("digest exceeds 128 characters")
	}
	return nil
}

func markdownTitle(markdown string) string {
	for _, line := range strings.Split(markdown, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	return ""
}

func digestFromHTML(content string, maxLength int) string {
	content = regexp.MustCompile("(?s)<[^>]+>").ReplaceAllString(content, " ")
	content = strings.Join(strings.Fields(stdhtml.UnescapeString(content)), " ")
	runes := []rune(content)
	if len(runes) > maxLength {
		return string(runes[:maxLength])
	}
	return content
}

func resolveLocalPath(baseDir, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(baseDir, path))
}

func normalizeNewlines(value string) string {
	value = strings.TrimPrefix(value, "\uFEFF")
	return strings.ReplaceAll(value, "\r\n", "\n")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func readiness(ready bool) string {
	if ready {
		return "ready"
	}
	return "blocked"
}

func check(level, code, message string) map[string]any {
	return map[string]any{"level": level, "code": code, "message": message}
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func writeJSON(value any) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(cliResponse{Success: true, SchemaVersion: "v1", Data: value})
}

func writeError(err error) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(cliResponse{Success: false, SchemaVersion: "v1", Error: err.Error()})
}
