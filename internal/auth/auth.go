package auth

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/proxy"
)

const (
	copilotClientID = "Iv1.b507a08c87ecfe98"
	userAgent       = "GitHubCopilotChat/0.26.7"
	defaultAPIBase  = "https://api.githubcopilot.com"
	tokenURL        = "https://api.github.com/copilot_internal/v2/token"
	deviceCodeURL   = "https://github.com/login/device/code"
	accessTokenURL  = "https://github.com/login/oauth/access_token"
)

var tokenExpRe = regexp.MustCompile(`exp=(\d+)`)

// TokenManager handles GitHub Copilot authentication.
type TokenManager struct {
	tokenFile     string
	httpClient    *http.Client
	cachedToken   string
	cachedExp     int64
	cachedAPIBase string
	refreshMu     sync.Mutex
}

func NewTokenManager(tokenFile, proxyAddr string) (*TokenManager, error) {
	transport, err := makeTransport(proxyAddr)
	if err != nil {
		return nil, fmt.Errorf("create transport: %w", err)
	}
	return &TokenManager{
		tokenFile: tokenFile,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   30 * time.Second,
		},
	}, nil
}

func makeTransport(proxyAddr string) (*http.Transport, error) {
	transport := &http.Transport{}
	if proxyAddr != "" {
		dialer, err := proxy.SOCKS5("tcp", proxyAddr, nil, proxy.Direct)
		if err != nil {
			return nil, fmt.Errorf("socks5 dialer: %w", err)
		}
		transport.DialContext = dialer.(proxy.ContextDialer).DialContext
	}
	return transport, nil
}

// SetTestToken sets the cached token for testing (skips GitHub OAuth).
func (tm *TokenManager) SetTestToken(token, apiBase string) {
	tm.cachedToken = token
	tm.cachedExp = time.Now().Unix() + 3600
	tm.cachedAPIBase = apiBase
}

// GetAPIBase returns the Copilot API base URL.
func (tm *TokenManager) GetAPIBase() string {
	if tm.cachedAPIBase != "" {
		return tm.cachedAPIBase
	}
	return defaultAPIBase
}

// GetToken returns a valid Copilot API token, refreshing if needed.
func (tm *TokenManager) GetToken() (string, error) {
	if tm.cachedToken != "" && time.Now().Unix() < tm.cachedExp-120 {
		return tm.cachedToken, nil
	}

	tm.refreshMu.Lock()
	defer tm.refreshMu.Unlock()

	if tm.cachedToken != "" && time.Now().Unix() < tm.cachedExp-120 {
		return tm.cachedToken, nil
	}

	gh, err := tm.readGitHubToken()
	if err != nil {
		return "", fmt.Errorf("read github token: %w", err)
	}

	req, _ := http.NewRequest("GET", tokenURL, nil)
	req.Header.Set("Authorization", "token "+gh)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Editor-Version", "vscode/1.104.1")
	req.Header.Set("Editor-Plugin-Version", "copilot-chat/0.26.7")
	req.Header.Set("Copilot-Integration-Id", "vscode-chat")

	resp, err := tm.doTokenRequest(req)
	if err != nil {
		return "", fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("token request failed: %d %s", resp.StatusCode, string(body))
	}

	var data struct {
		Token     string `json:"token"`
		Endpoints struct {
			API string `json:"api"`
		} `json:"endpoints"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return "", fmt.Errorf("parse token response: %w", err)
	}

	tm.cachedToken = data.Token
	if data.Endpoints.API != "" {
		tm.cachedAPIBase = data.Endpoints.API
	}
	if m := tokenExpRe.FindStringSubmatch(data.Token); m != nil {
		tm.cachedExp, _ = strconv.ParseInt(m[1], 10, 64)
	}
	if tm.cachedExp == 0 {
		tm.cachedExp = time.Now().Unix() + 1500
	}
	return tm.cachedToken, nil
}

// CheckTokenAvailable returns true if a non-empty GitHub token file exists.
func (tm *TokenManager) CheckTokenAvailable() bool {
	data, err := os.ReadFile(tm.tokenFile)
	if err != nil {
		return false
	}
	return len(strings.TrimSpace(string(data))) > 0
}

func (tm *TokenManager) readGitHubToken() (string, error) {
	data, err := os.ReadFile(tm.tokenFile)
	if err != nil {
		return "", fmt.Errorf("no github token at %s — run 'onellm-router login' first", tm.tokenFile)
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", fmt.Errorf("empty github token at %s", tm.tokenFile)
	}
	return token, nil
}

// Logout removes the GitHub token file and clears cached credentials.
func (tm *TokenManager) Logout() error {
	tm.refreshMu.Lock()
	defer tm.refreshMu.Unlock()
	tm.cachedToken = ""
	tm.cachedExp = 0
	tm.cachedAPIBase = ""
	return os.Remove(tm.tokenFile)
}

// DeviceLogin runs the GitHub device OAuth flow and saves the token (blocking).
func (tm *TokenManager) DeviceLogin() error {
	dir := filepath.Dir(tm.tokenFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create token dir: %w", err)
	}

	form1 := url.Values{
		"client_id": {copilotClientID},
		"scope":     {"read:user"},
	}
	req1, _ := http.NewRequest("POST", deviceCodeURL, strings.NewReader(form1.Encode()))
	req1.Header.Set("Accept", "application/json")
	req1.Header.Set("User-Agent", userAgent)
	req1.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp1, err := tm.httpClient.Do(req1)
	if err != nil {
		return fmt.Errorf("device code request: %w", err)
	}
	defer resp1.Body.Close()

	body1, _ := io.ReadAll(io.LimitReader(resp1.Body, 4096))
	if resp1.StatusCode != 200 {
		return fmt.Errorf("device code request failed: %d %s", resp1.StatusCode, string(body1))
	}

	var dc struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		Interval        int    `json:"interval"`
	}
	if err := json.Unmarshal(body1, &dc); err != nil {
		return fmt.Errorf("parse device code: %w", err)
	}

	fmt.Println()
	fmt.Println("请打开以下链接完成 GitHub 设备授权：")
	fmt.Printf("   %s\n", dc.VerificationURI)
	fmt.Printf("   输入验证码: %s\n", dc.UserCode)
	fmt.Println()

	interval := dc.Interval
	if interval < 5 {
		interval = 5
	}

	for {
		time.Sleep(time.Duration(interval) * time.Second)

		form2 := url.Values{
			"client_id":   {copilotClientID},
			"device_code": {dc.DeviceCode},
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		}
		req2, _ := http.NewRequest("POST", accessTokenURL, strings.NewReader(form2.Encode()))
		req2.Header.Set("Accept", "application/json")
		req2.Header.Set("User-Agent", userAgent)
		req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		resp2, err := tm.httpClient.Do(req2)
		if err != nil {
			return fmt.Errorf("access token request: %w", err)
		}
		body2, _ := io.ReadAll(io.LimitReader(resp2.Body, 4096))
		resp2.Body.Close()

		var at struct {
			AccessToken string `json:"access_token"`
			Error       string `json:"error"`
		}
		if err := json.Unmarshal(body2, &at); err != nil {
			continue
		}

		switch at.Error {
		case "":
			if at.AccessToken != "" {
				if err := os.WriteFile(tm.tokenFile, []byte(at.AccessToken+"\n"), 0600); err != nil {
					return fmt.Errorf("write token file: %w", err)
				}
				fmt.Println("GitHub 授权成功")
				return nil
			}
		case "authorization_pending":
			continue
		case "slow_down":
			interval += 5
			continue
		default:
			return fmt.Errorf("device login failed: %s", at.Error)
		}
	}
}

// doTokenRequest retries the Copilot token endpoint up to 3 times.
func (tm *TokenManager) doTokenRequest(req *http.Request) (*http.Response, error) {
	var lastErr error
	for i := 0; i < 3; i++ {
		resp, err := tm.httpClient.Do(req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if i < 2 {
			time.Sleep(time.Duration(1<<uint(i)) * time.Second) // 1s, 2s
		}
	}
	return nil, lastErr
}


