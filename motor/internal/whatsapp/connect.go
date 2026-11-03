package whatsapp

// This file holds the two Meta-side steps that no credential shows and that
// silently break delivery when they are missing:
//
//  1. the app's webhook subscription (callback URL + token + fields), and
//  2. the app being subscribed to the WhatsApp Business account (WABA).
//
// With them done from here the owner only pastes credentials in the panel.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// WebhookSpec is the app's webhook registration for WhatsApp.
type WebhookSpec struct {
	Active      bool
	CallbackURL string
	Fields      []string
}

// doAuth runs one Graph request with an explicit bearer token.
func (c *Client) doAuth(ctx context.Context, method, path, bearer string, form url.Values) ([]byte, error) {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, c.endpoint(path), body)
	if err != nil {
		return nil, err
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Meta HTTP %d: %s", resp.StatusCode, apiError(raw))
	}
	return raw, nil
}

// AppID returns the Meta app behind the access token. It asks Meta rather than
// the owner, because the App ID is easy to confuse with the phone number ID.
func (c *Client) AppID(ctx context.Context) (string, error) {
	if c == nil || c.Token == "" {
		return "", ErrNotConfigured
	}
	q := url.Values{"input_token": {c.Token}, "access_token": {c.Token}}
	raw, err := c.doAuth(ctx, http.MethodGet, "debug_token?"+q.Encode(), "", nil)
	if err != nil {
		return "", err
	}
	var out struct {
		Data struct {
			AppID string `json:"app_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	if out.Data.AppID == "" {
		return "", fmt.Errorf("Meta no dijo a qué app pertenece el token")
	}
	return out.Data.AppID, nil
}

// appToken is the app access token Meta wants for app-level configuration.
func appToken(appID, appSecret string) string { return appID + "|" + appSecret }

// SubscribedApps lists the apps that receive this WABA's webhooks.
func (c *Client) SubscribedApps(ctx context.Context, wabaID string) ([]string, error) {
	raw, err := c.doAuth(ctx, http.MethodGet, wabaID+"/subscribed_apps", c.Token, nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		Data []struct {
			ID string `json:"id"`
			What struct {
				ID string `json:"id"`
			} `json:"whatsapp_business_api_data"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(out.Data))
	for _, d := range out.Data {
		if d.What.ID != "" {
			ids = append(ids, d.What.ID)
		} else if d.ID != "" {
			ids = append(ids, d.ID)
		}
	}
	return ids, nil
}

// SubscribeApp links this app to the WABA, so Meta forwards its messages here.
// It is idempotent: subscribing twice is not an error.
func (c *Client) SubscribeApp(ctx context.Context, wabaID string) error {
	_, err := c.doAuth(ctx, http.MethodPost, wabaID+"/subscribed_apps", c.Token, url.Values{})
	return err
}

// Webhook reads the app's current webhook for WhatsApp ("" app fields = none).
func (c *Client) Webhook(ctx context.Context, appID, secret string) (WebhookSpec, error) {
	raw, err := c.doAuth(ctx, http.MethodGet, appID+"/subscriptions?object=whatsapp_business_account",
		appToken(appID, secret), nil)
	if err != nil {
		return WebhookSpec{}, err
	}
	var out struct {
		Data []struct {
			CallbackURL string `json:"callback_url"`
			Active      bool   `json:"active"`
			Fields      []struct {
				Name string `json:"name"`
			} `json:"fields"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return WebhookSpec{}, err
	}
	if len(out.Data) == 0 {
		return WebhookSpec{}, nil
	}
	spec := WebhookSpec{CallbackURL: out.Data[0].CallbackURL, Active: out.Data[0].Active}
	for _, f := range out.Data[0].Fields {
		spec.Fields = append(spec.Fields, f.Name)
	}
	return spec, nil
}

// SetWebhook registers the callback URL, the verify token and the "messages"
// field on the app. Meta verifies the URL while handling this call, so a nil
// error means Meta really reached the Motor.
func (c *Client) SetWebhook(ctx context.Context, appID, secret, callbackURL, verifyToken string, fields []string) error {
	if len(fields) == 0 {
		fields = []string{"messages"}
	}
	form := url.Values{
		"object":       {"whatsapp_business_account"},
		"callback_url": {callbackURL},
		"verify_token": {verifyToken},
		"fields":       {strings.Join(fields, ",")},
	}
	_, err := c.doAuth(ctx, http.MethodPost, appID+"/subscriptions", appToken(appID, secret), form)
	return err
}

// AccountIDs returns the WhatsApp Business account ids carried by a webhook.
// Meta always includes one in entry[].id, which is how the Motor learns the
// WABA id without asking the owner for a second identifier.
func AccountIDs(body []byte) []string {
	var p struct {
		Entry []struct {
			ID string `json:"id"`
		} `json:"entry"`
	}
	if json.Unmarshal(body, &p) != nil {
		return nil
	}
	var ids []string
	seen := map[string]bool{}
	for _, e := range p.Entry {
		if e.ID != "" && !seen[e.ID] {
			seen[e.ID] = true
			ids = append(ids, e.ID)
		}
	}
	return ids
}
