package wechat

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/geekjourneyx/md2wechat-skill/projects/md2wechat-publish/internal/config"
	"github.com/silenceper/wechat/v2"
	wechatcache "github.com/silenceper/wechat/v2/cache"
	"github.com/silenceper/wechat/v2/officialaccount"
	wechatconfig "github.com/silenceper/wechat/v2/officialaccount/config"
	"github.com/silenceper/wechat/v2/officialaccount/draft"
	"github.com/silenceper/wechat/v2/officialaccount/material"
	"github.com/silenceper/wechat/v2/util"
	"go.uber.org/zap"
)

var (
	downloadLookupIP      = net.LookupIP
	wechatSDKHTTPClientMu sync.Mutex
	newDownloadHTTPClient = func() *http.Client {
		return &http.Client{
			Timeout: 60 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return errors.New("stopped after 5 redirects")
				}
				return validateRemoteDownloadURL(req.URL)
			},
		}
	}
)

// Service 微信服务
type Service struct {
	cfg                *config.Config
	log                *zap.Logger
	wc                 *wechat.Wechat
	httpClient         *http.Client
	httpClientErr      error
	uploadMaterialFunc func(string) (*UploadMaterialResult, error)
}

// NewService 创建微信服务
func NewService(cfg *config.Config, log *zap.Logger) *Service {
	httpClient, httpClientErr := newWechatHTTPClient(cfg)

	return &Service{
		cfg:           cfg,
		log:           log,
		wc:            wechat.NewWechat(),
		httpClient:    httpClient,
		httpClientErr: httpClientErr,
	}
}

func newWechatHTTPClient(cfg *config.Config) (*http.Client, error) {
	timeout := 60 * time.Second
	if cfg != nil && cfg.HTTPTimeout > 0 {
		timeout = time.Duration(cfg.HTTPTimeout) * time.Second
	}

	client := &http.Client{Timeout: timeout}
	if cfg == nil || strings.TrimSpace(cfg.WechatProxyURL) == "" {
		return client, nil
	}

	proxyURL, err := neturl.Parse(strings.TrimSpace(cfg.WechatProxyURL))
	if err != nil {
		return client, fmt.Errorf("wechat proxy url: %w", err)
	}
	if proxyURL.Scheme != "http" && proxyURL.Scheme != "https" {
		return client, fmt.Errorf("wechat proxy url: unsupported scheme %q", proxyURL.Scheme)
	}
	if proxyURL.Hostname() == "" {
		return client, fmt.Errorf("wechat proxy url: missing host")
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyURL(proxyURL)
	client.Transport = transport
	return client, nil
}

// getOfficialAccount 获取公众号实例
func (s *Service) getOfficialAccount() *officialaccount.OfficialAccount {
	memory := wechatcache.NewMemory()
	wechatCfg := &wechatconfig.Config{
		AppID:     s.cfg.WechatAppID,
		AppSecret: s.cfg.WechatSecret,
		Cache:     memory,
	}
	return s.wc.GetOfficialAccount(wechatCfg)
}

// UploadMaterialResult 上传素材结果
type UploadMaterialResult struct {
	MediaID   string `json:"media_id"`
	WechatURL string `json:"wechat_url"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
}

// UploadMaterial 上传素材到微信
func (s *Service) UploadMaterial(filePath string) (*UploadMaterialResult, error) {
	if s.uploadMaterialFunc != nil {
		return s.uploadMaterialFunc(filePath)
	}
	var result *UploadMaterialResult
	err := s.withWechatSDKHTTPClient(func() error {
		startTime := time.Now()
		oa := s.getOfficialAccount()
		mat := oa.GetMaterial()

		// 调用微信 API 上传（SDK 接受文件路径字符串）
		mediaID, url, err := mat.AddMaterial(material.MediaTypeImage, filePath)
		if err != nil {
			s.log.Error("upload material failed",
				zap.String("path", filePath),
				zap.Error(err))
			return fmt.Errorf("upload material: %w", err)
		}

		duration := time.Since(startTime)
		s.log.Info("material uploaded",
			zap.String("path", filePath),
			zap.String("media_id", maskMediaID(mediaID)),
			zap.Duration("duration", duration))

		result = &UploadMaterialResult{
			MediaID:   mediaID,
			WechatURL: url,
		}
		return nil
	})
	return result, err
}

// CreateDraftResult 创建草稿结果
type CreateDraftResult struct {
	MediaID  string `json:"media_id"`
	DraftURL string `json:"draft_url,omitempty"`
}

// CreateDraft 创建草稿
func (s *Service) CreateDraft(articles []*draft.Article) (*CreateDraftResult, error) {
	var result *CreateDraftResult
	err := s.withWechatSDKHTTPClient(func() error {
		startTime := time.Now()
		oa := s.getOfficialAccount()
		dm := oa.GetDraft()

		// 直接调用 SDK 方法，SDK 接受 []*draft.Article
		mediaID, err := dm.AddDraft(articles)
		if err != nil {
			s.log.Error("create draft failed", zap.Error(err))
			return fmt.Errorf("create draft: %w", ExplainDraftError(err))
		}

		duration := time.Since(startTime)
		s.log.Info("draft created",
			zap.String("media_id", maskMediaID(mediaID)),
			zap.Duration("duration", duration))

		result = &CreateDraftResult{
			MediaID: mediaID,
		}
		return nil
	})
	return result, err
}

// maskMediaID 遮蔽 media_id 用于日志
func maskMediaID(id string) string {
	if id == "" || len(id) < 8 {
		return "***"
	}
	return id[:4] + "***" + id[len(id)-4:]
}

// DownloadFile 下载文件到临时目录，或返回本地文件路径
// 如果传入的是本地文件路径（不以 http:// 或 https:// 开头），则直接返回该路径
func DownloadFile(urlOrPath string) (string, error) {
	// 检查是否是本地文件路径（不是 HTTP URL）
	if !strings.HasPrefix(urlOrPath, "http://") && !strings.HasPrefix(urlOrPath, "https://") {
		// 本地文件 - 检查是否存在
		if _, err := os.Stat(urlOrPath); err == nil {
			return urlOrPath, nil // 直接返回本地路径
		}
		return "", fmt.Errorf("local file not found: %s", urlOrPath)
	}

	// HTTP URL - 下载文件
	url := urlOrPath
	parsedURL, err := neturl.Parse(url)
	if err != nil {
		return "", fmt.Errorf("parse download url: %w", err)
	}
	if err := validateRemoteDownloadURL(parsedURL); err != nil {
		return "", err
	}

	// 创建 HTTP 客户端
	client := newDownloadHTTPClient()

	// 发起请求
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("download file: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download failed with status: %d", resp.StatusCode)
	}

	// 从 URL 路径中提取扩展名，排除查询参数
	ext := ".jpg" // 默认扩展名
	if pathExt := filepath.Ext(parsedURL.Path); pathExt != "" {
		ext = pathExt
	}
	tmpFile, err := os.CreateTemp("", "md2wechat-download-*"+ext)
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	// 写入文件
	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("write file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("close temp file: %w", err)
	}

	return tmpPath, nil
}

func validateRemoteDownloadURL(parsedURL *neturl.URL) error {
	if parsedURL == nil {
		return fmt.Errorf("invalid download url")
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return fmt.Errorf("unsupported download scheme: %s", parsedURL.Scheme)
	}

	host := parsedURL.Hostname()
	if host == "" {
		return fmt.Errorf("download url missing host")
	}
	if err := validateDownloadPort(parsedURL.Port()); err != nil {
		return err
	}
	if err := validateDownloadHost(host); err != nil {
		return err
	}
	return nil
}

func validateDownloadPort(port string) error {
	if port == "" || port == "80" || port == "443" {
		return nil
	}
	return fmt.Errorf("download url uses disallowed port: %s", port)
}

func validateDownloadHost(host string) error {
	lowerHost := strings.ToLower(strings.TrimSpace(host))
	if lowerHost == "" {
		return fmt.Errorf("download url missing host")
	}
	if lowerHost == "localhost" || strings.HasSuffix(lowerHost, ".localhost") {
		return fmt.Errorf("download host is not allowed: %s", host)
	}

	if ip := net.ParseIP(lowerHost); ip != nil {
		if err := validateDownloadIP(ip); err != nil {
			return fmt.Errorf("download host is not allowed: %w", err)
		}
		return nil
	}

	ips, err := downloadLookupIP(host)
	if err != nil {
		return fmt.Errorf("resolve download host %s: %w", host, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("resolve download host %s: no addresses found", host)
	}
	for _, ip := range ips {
		if err := validateDownloadIP(ip); err != nil {
			return fmt.Errorf("download host is not allowed: %w", err)
		}
	}
	return nil
}

func validateDownloadIP(ip net.IP) error {
	if ip == nil {
		return fmt.Errorf("invalid ip")
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return fmt.Errorf("ip %s is private or local", ip.String())
	}
	return nil
}

func (s *Service) getHTTPClient() *http.Client {
	if s != nil && s.httpClient != nil {
		return s.httpClient
	}
	return &http.Client{Timeout: 60 * time.Second}
}

func (s *Service) ensureHTTPClientReady() error {
	if s != nil && s.httpClientErr != nil {
		return s.httpClientErr
	}
	return nil
}

func (s *Service) withWechatSDKHTTPClient(fn func() error) error {
	if err := s.ensureHTTPClientReady(); err != nil {
		return err
	}

	wechatSDKHTTPClientMu.Lock()
	previousClient := util.DefaultHTTPClient
	// util.DefaultHTTPClient is SDK-global; install the service client only during WeChat side-effect operations.
	util.DefaultHTTPClient = s.getHTTPClient()
	defer func() {
		util.DefaultHTTPClient = previousClient
		wechatSDKHTTPClientMu.Unlock()
	}()

	return fn()
}
