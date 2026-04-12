package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	authzcore "github.com/geoffbelknap/agency/internal/authz"
	agencyconsent "github.com/geoffbelknap/agency/internal/consent"
)

type AuthorityHandler struct {
	Manifest         *Manifest
	Resolver         authzcore.Resolver
	ConsentValidator *agencyconsent.Validator
}

type AuthorityInvokeRequest struct {
	Subject         string         `json:"subject"`
	NodeID          string         `json:"node_id"`
	Action          string         `json:"action"`
	ConsentProvided bool           `json:"consent_provided,omitempty"`
	Input           map[string]any `json:"input,omitempty"`
}

type AuthorityInvokeResponse struct {
	Allowed    bool               `json:"allowed"`
	Decision   authzcore.Decision `json:"decision"`
	Execution  string             `json:"execution"`
	StatusCode int                `json:"status_code,omitempty"`
	Result     any                `json:"result,omitempty"`
	Descriptor map[string]any     `json:"descriptor,omitempty"`
}

func (h AuthorityHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/health":
		h.writeJSON(w, http.StatusOK, map[string]any{
			"status":      "ok",
			"instance_id": h.Manifest.Metadata.InstanceID,
			"manifest_id": h.Manifest.Metadata.ManifestID,
		})
	case r.Method == http.MethodGet && r.URL.Path == "/tools":
		h.writeJSON(w, http.StatusOK, map[string]any{"nodes": h.Manifest.Runtime.Nodes})
	case r.Method == http.MethodPost && r.URL.Path == "/invoke":
		h.invoke(w, r)
	default:
		h.writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}

func (h AuthorityHandler) invoke(w http.ResponseWriter, r *http.Request) {
	var req AuthorityInvokeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	node, err := findAuthorityNode(h.Manifest, req.NodeID)
	if err != nil {
		h.writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	if err := h.validateConsent(&req, node); err != nil {
		if errors.Is(err, agencyconsent.ErrTokenMissing) {
			req.ConsentProvided = false
		} else if errors.Is(err, errConsentInputMalformed) {
			h.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		} else {
			h.writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
			return
		}
	}
	target := "node:" + h.Manifest.Metadata.InstanceName + "/" + req.NodeID
	authzReq := ResolveRequestAgainstManifest(h.Manifest, authzcore.Request{
		Subject:         req.Subject,
		Target:          target,
		Action:          req.Action,
		Instance:        h.Manifest.Metadata.InstanceName,
		ConsentProvided: req.ConsentProvided,
	})
	decision, err := h.Resolver.Resolve(authzReq)
	if err != nil {
		h.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if !decision.Allow {
		h.writeJSON(w, http.StatusForbidden, AuthorityInvokeResponse{
			Allowed:   false,
			Decision:  decision,
			Execution: "denied",
		})
		return
	}
	executed, err := ExecuteAuthority(context.Background(), h.Manifest, node, req)
	if err != nil {
		h.writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	if executed != nil {
		h.writeJSON(w, http.StatusOK, AuthorityInvokeResponse{
			Allowed:    true,
			Decision:   decision,
			Execution:  "executed",
			StatusCode: executed.StatusCode,
			Result:     firstNonNil(executed.Body, executed.RawBody),
			Descriptor: map[string]any{
				"node_id": req.NodeID,
				"action":  req.Action,
			},
		})
		return
	}
	h.writeJSON(w, http.StatusNotImplemented, AuthorityInvokeResponse{
		Allowed:   true,
		Decision:  decision,
		Execution: "not_implemented",
		Descriptor: map[string]any{
			"node_id": req.NodeID,
			"action":  req.Action,
			"input":   req.Input,
		},
	})
}

var errConsentInputMalformed = errors.New("consent_input_malformed")

func (h AuthorityHandler) validateConsent(req *AuthorityInvokeRequest, node *RuntimeNode) error {
	if node == nil || req == nil {
		return nil
	}
	requirement, ok := node.ConsentRequirements[req.Action]
	if !ok {
		return nil
	}
	tokenValue, targetValue, err := extractConsentInputs(req.Input, requirement)
	if err != nil {
		return errors.Join(errConsentInputMalformed, err)
	}
	if tokenValue == "" {
		return agencyconsent.ErrTokenMissing
	}
	if h.ConsentValidator == nil {
		return agencyconsent.ErrVerifierUnavailable
	}
	if _, err := h.ConsentValidator.Validate(requirement, tokenValue, targetValue, time.Now().UTC()); err != nil {
		return err
	}
	req.ConsentProvided = true
	return nil
}

func extractConsentInputs(input map[string]any, requirement agencyconsent.Requirement) (string, string, error) {
	requirement = requirement.Normalize()
	if requirement.OperationKind == "" {
		return "", "", nil
	}
	if input == nil {
		return "", "", nil
	}
	tokenValue, _ := input[requirement.TokenInputField].(string)
	targetRaw, ok := input[requirement.TargetInputField]
	if !ok || targetRaw == nil {
		return tokenValue, "", nil
	}
	switch value := targetRaw.(type) {
	case string:
		return tokenValue, value, nil
	case float64, bool, int, int64:
		return tokenValue, fmt.Sprintf("%v", value), nil
	default:
		targetValue, err := stringifyConsentTarget(value)
		return tokenValue, targetValue, err
	}
}

func stringifyConsentTarget(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	var asString string
	if err := json.Unmarshal(data, &asString); err == nil {
		return asString, nil
	}
	return string(data), nil
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value == nil {
			continue
		}
		if s, ok := value.(string); ok && s == "" {
			continue
		}
		return value
	}
	return nil
}

func (h AuthorityHandler) writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}
