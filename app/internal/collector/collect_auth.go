package collector

import (
    "fmt"
    "strings"
)

// Authenticate performs a `user.login` request and returns the auth token.
// It uses the provided ApiRequester so callers can inject the application's
// JSON-RPC wrapper (e.g., zabbixApiRequest).
func Authenticate(apiUrl, username, password string, req ApiRequester) (string, error) {
    resp, err := req(apiUrl, "", "user.login", map[string]interface{}{"username": username, "password": password})
    if err != nil {
        return "", err
    }
    if r, ok := resp["result"]; ok {
        if tok, ok2 := r.(string); ok2 {
            if strings.TrimSpace(tok) != "" {
                return tok, nil
            }
            return "", fmt.Errorf("empty token returned")
        }
    }
    if errObj, ok := resp["error"]; ok {
        return "", fmt.Errorf("login error: %v", errObj)
    }
    return "", fmt.Errorf("unexpected login response: %v", resp)
}
