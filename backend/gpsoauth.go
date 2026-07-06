package backend

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	// gpsoauthURL is Google's device-account auth endpoint (same one gotohp already
	// calls via getAuthToken to mint a bearer). Its "ac2dm" service returns a master
	// token from a web oauth_token.
	gpsoauthURL = "https://android.clients.google.com/auth"

	// googlePhotosClientSig is the SHA-1 signature of the official Google Photos
	// APK; Google's auth endpoint keys the response to this value.
	googlePhotosClientSig = "24bb24c05e47e0aefa68a58a766179d9b613a600"

	// photosNativeScope is the full oauth2 scope string the Google Photos Android
	// app uses. Same string gotohp expects to see in a stored auth string.
	photosNativeScope = "oauth2:openid https://www.googleapis.com/auth/mobileapps.native https://www.googleapis.com/auth/photos.native"

	// gpsoauthTimeout bounds the auth exchange so a stalled/blackholed request
	// cannot hang the login flow forever (the shared HTTP client uses Timeout: 0).
	gpsoauthTimeout = 30 * time.Second

	// gpsoauthMaxBody caps the response body size to defend against a misbehaving
	// proxy or upstream returning an unbounded stream. Legitimate responses are
	// only a few hundred bytes.
	gpsoauthMaxBody = 1 << 20 // 1 MiB
)

// RandomAndroidID returns a 16-hex-char device identifier.
func RandomAndroidID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// gpsoauthPost is a shared wrapper around Google's /auth endpoint. Response body
// is a text/plain key=value listing. Times out after gpsoauthTimeout.
func gpsoauthPost(form url.Values) (map[string]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gpsoauthTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", gpsoauthURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "GoogleAuth/1.4")

	client, err := NewHTTPClientWithProxy(AppConfig.Proxy)
	if err != nil {
		return nil, fmt.Errorf("http client: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, gpsoauthMaxBody))
	if err != nil {
		return nil, err
	}

	out := make(map[string]string, 16)
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if eq := strings.IndexByte(line, '='); eq > 0 {
			out[strings.TrimSpace(line[:eq])] = strings.TrimSpace(line[eq+1:])
		}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Do not return `out`: it may carry Token / Auth on partial-success
		// responses and callers must not accidentally leak that on an error path.
		detail := out["Error"]
		if detail == "" {
			detail = out["ErrorDetail"]
		}
		return nil, fmt.Errorf("auth endpoint returned %d: %s", resp.StatusCode, detail)
	}
	return out, nil
}

// ExchangeOAuthToken converts a one-time oauth_token cookie value (captured
// from an EmbeddedSetup browser login) into a long-lived aas_et master token.
// The email in the response is authoritative.
func ExchangeOAuthToken(oauthToken, androidID string) (email string, masterToken string, err error) {
	form := url.Values{}
	form.Set("accountType", "HOSTED_OR_GOOGLE")
	// Email is filled in by the server based on the oauth_token; empty is fine.
	form.Set("Email", "")
	form.Set("has_permission", "1")
	form.Set("add_account", "1")
	form.Set("ACCESS_TOKEN", "1")
	form.Set("Token", oauthToken)
	form.Set("service", "ac2dm")
	form.Set("source", "android")
	form.Set("androidId", androidID)
	form.Set("device_country", "us")
	form.Set("operatorCountry", "us")
	form.Set("lang", "en")
	form.Set("sdk_version", "29")
	form.Set("google_play_services_version", "240913000")
	form.Set("client_sig", googlePhotosClientSig)
	form.Set("callerSig", googlePhotosClientSig)
	form.Set("droidguard_results", "dummy123")

	r, err := gpsoauthPost(form)
	if err != nil {
		return "", "", err
	}
	if r["Token"] == "" {
		return "", "", fmt.Errorf("exchange returned no master Token (got fields: %v)", sortedKeys(r))
	}
	respEmail := r["Email"]
	if respEmail == "" {
		return "", "", fmt.Errorf("exchange did not return an Email field; refusing to store an anonymous credential")
	}
	return respEmail, r["Token"], nil
}

// BuildAuthStringFromMasterToken assembles the gotohp-format credential query
// string that AddCredentials accepts.
func BuildAuthStringFromMasterToken(email, masterToken, androidID string) string {
	q := url.Values{}
	q.Set("androidId", androidID)
	q.Set("app", "com.google.android.apps.photos")
	q.Set("client_sig", googlePhotosClientSig)
	q.Set("callerPkg", "com.google.android.apps.photos")
	q.Set("callerSig", googlePhotosClientSig)
	q.Set("device_country", "us")
	q.Set("Email", email)
	q.Set("google_play_services_version", "240913000")
	q.Set("lang", "en")
	q.Set("oauth2_foreground", "1")
	q.Set("operatorCountry", "us")
	q.Set("sdk_version", "29")
	q.Set("service", photosNativeScope)
	q.Set("source", "android")
	q.Set("Token", masterToken)
	return q.Encode()
}

// sortedKeys returns a stable, sorted list of map keys for error messages.
// Local helper to avoid a package-wide generic identifier.
func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
