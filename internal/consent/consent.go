package consent

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/fxamacker/cbor/v2"
)

const DefaultClockSkew = 30 * time.Second

var (
	ErrTokenMissing        = errors.New("consent_token_missing")
	ErrTokenMalformed      = errors.New("consent_token_malformed")
	ErrUnknownKey          = errors.New("consent_token_unknown_key")
	ErrInvalidSignature    = errors.New("consent_token_invalid_signature")
	ErrWrongDeployment     = errors.New("consent_token_wrong_deployment")
	ErrExpired             = errors.New("consent_token_expired")
	ErrWrongOperation      = errors.New("consent_token_wrong_operation")
	ErrWrongTarget         = errors.New("consent_token_wrong_target")
	ErrTTLExceeded         = errors.New("consent_token_ttl_exceeded")
	ErrInsufficientWitness = errors.New("consent_token_insufficient_witnesses")
	ErrReplayed            = errors.New("consent_token_replayed")
	ErrVerifierUnavailable = errors.New("consent_token_verifier_unavailable")
	canonicalCBOR, _       = cbor.CanonicalEncOptions().EncMode()
)

type Requirement struct {
	OperationKind    string `yaml:"operation_kind" json:"operation_kind"`
	TokenInputField  string `yaml:"token_input_field" json:"token_input_field"`
	TargetInputField string `yaml:"target_input_field" json:"target_input_field"`
	MinWitnesses     int    `yaml:"min_witnesses,omitempty" json:"min_witnesses,omitempty"`
}

func (r Requirement) Normalize() Requirement {
	if r.MinWitnesses <= 0 {
		r.MinWitnesses = 1
	}
	return r
}

func (r Requirement) Validate() error {
	if strings.TrimSpace(r.OperationKind) == "" {
		return fmt.Errorf("operation_kind is required")
	}
	if strings.TrimSpace(r.TokenInputField) == "" {
		return fmt.Errorf("token_input_field is required")
	}
	if strings.TrimSpace(r.TargetInputField) == "" {
		return fmt.Errorf("target_input_field is required")
	}
	return nil
}

type Token struct {
	Version         uint8    `cbor:"1,keyasint"`
	DeploymentID    string   `cbor:"2,keyasint"`
	OperationKind   string   `cbor:"3,keyasint"`
	OperationTarget []byte   `cbor:"4,keyasint"`
	Issuer          string   `cbor:"5,keyasint"`
	Witnesses       []string `cbor:"6,keyasint"`
	IssuedAt        int64    `cbor:"7,keyasint"`
	ExpiresAt       int64    `cbor:"8,keyasint"`
	Nonce           []byte   `cbor:"9,keyasint"`
	SigningKeyID    string   `cbor:"10,keyasint"`
}

type SignedToken struct {
	Token     Token  `cbor:"1,keyasint"`
	Signature []byte `cbor:"2,keyasint"`
}

func (t Token) MarshalCanonical() ([]byte, error) {
	return canonicalCBOR.Marshal(t)
}

func EncodeSignedToken(s SignedToken) (string, error) {
	data, err := canonicalCBOR.Marshal(s)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func DecodeSignedToken(encoded string) (*SignedToken, error) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTokenMalformed, err)
	}
	var signed SignedToken
	if err := cbor.Unmarshal(raw, &signed); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTokenMalformed, err)
	}
	if signed.Token.Version == 0 {
		return nil, fmt.Errorf("%w: missing version", ErrTokenMalformed)
	}
	return &signed, nil
}

type Result struct {
	Token        Token
	NonceEncoded string
	Requirement  Requirement
}

type Validator struct {
	mu             sync.Mutex
	deploymentID   string
	publicKeys     map[string]ed25519.PublicKey
	maxTTL         time.Duration
	clockSkew      time.Duration
	consumedNonces map[string]map[string]int64
}

func NewValidator(deploymentID string, keys map[string]ed25519.PublicKey, maxTTL, clockSkew time.Duration) *Validator {
	if clockSkew <= 0 {
		clockSkew = DefaultClockSkew
	}
	if maxTTL <= 0 {
		maxTTL = 15 * time.Minute
	}
	cloned := make(map[string]ed25519.PublicKey, len(keys))
	for k, v := range keys {
		keyCopy := make([]byte, len(v))
		copy(keyCopy, v)
		cloned[k] = ed25519.PublicKey(keyCopy)
	}
	return &Validator{
		deploymentID:   deploymentID,
		publicKeys:     cloned,
		maxTTL:         maxTTL,
		clockSkew:      clockSkew,
		consumedNonces: make(map[string]map[string]int64),
	}
}

func (v *Validator) Validate(requirement Requirement, tokenEncoded string, target string, now time.Time) (*Result, error) {
	requirement = requirement.Normalize()
	if err := requirement.Validate(); err != nil {
		return nil, err
	}
	if v == nil || len(v.publicKeys) == 0 || strings.TrimSpace(v.deploymentID) == "" {
		return nil, ErrVerifierUnavailable
	}
	if strings.TrimSpace(tokenEncoded) == "" {
		return nil, ErrTokenMissing
	}

	signed, err := DecodeSignedToken(tokenEncoded)
	if err != nil {
		return nil, err
	}
	pubkey, ok := v.publicKeys[signed.Token.SigningKeyID]
	if !ok {
		return nil, ErrUnknownKey
	}
	tokenBytes, err := signed.Token.MarshalCanonical()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTokenMalformed, err)
	}
	if !ed25519.Verify(pubkey, tokenBytes, signed.Signature) {
		return nil, ErrInvalidSignature
	}
	if signed.Token.DeploymentID != v.deploymentID {
		return nil, ErrWrongDeployment
	}

	nowMs := now.UnixMilli()
	if nowMs > signed.Token.ExpiresAt+v.clockSkew.Milliseconds() {
		return nil, ErrExpired
	}
	if signed.Token.ExpiresAt < signed.Token.IssuedAt {
		return nil, ErrTokenMalformed
	}
	if time.Duration(signed.Token.ExpiresAt-signed.Token.IssuedAt)*time.Millisecond > v.maxTTL {
		return nil, ErrTTLExceeded
	}
	if signed.Token.OperationKind != requirement.OperationKind {
		return nil, ErrWrongOperation
	}
	if string(signed.Token.OperationTarget) != target {
		return nil, ErrWrongTarget
	}
	if len(signed.Token.Witnesses) < requirement.MinWitnesses {
		return nil, ErrInsufficientWitness
	}

	nonceEncoded := base64.RawURLEncoding.EncodeToString(signed.Token.Nonce)
	if v.consumeNonce(signed.Token.DeploymentID, nonceEncoded, signed.Token.ExpiresAt, nowMs) {
		return nil, ErrReplayed
	}

	return &Result{
		Token:        signed.Token,
		NonceEncoded: nonceEncoded,
		Requirement:  requirement,
	}, nil
}

func (v *Validator) consumeNonce(deploymentID, nonce string, expiresAtMs, nowMs int64) bool {
	v.mu.Lock()
	defer v.mu.Unlock()

	if _, ok := v.consumedNonces[deploymentID]; !ok {
		v.consumedNonces[deploymentID] = make(map[string]int64)
	}
	for seenNonce, expiry := range v.consumedNonces[deploymentID] {
		if nowMs > expiry+v.clockSkew.Milliseconds() {
			delete(v.consumedNonces[deploymentID], seenNonce)
		}
	}
	if _, exists := v.consumedNonces[deploymentID][nonce]; exists {
		return true
	}
	v.consumedNonces[deploymentID][nonce] = expiresAtMs
	return false
}

func ParsePublicKey(encoded string) (ed25519.PublicKey, error) {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return nil, fmt.Errorf("empty public key")
	}
	candidates := []func(string) ([]byte, error){
		base64.RawURLEncoding.DecodeString,
		base64.StdEncoding.DecodeString,
		hex.DecodeString,
	}
	for _, decode := range candidates {
		raw, err := decode(encoded)
		if err == nil && len(raw) == ed25519.PublicKeySize {
			return ed25519.PublicKey(raw), nil
		}
	}
	return nil, fmt.Errorf("invalid Ed25519 public key encoding")
}

func GenerateSigningKeyPair() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	return ed25519.GenerateKey(rand.Reader)
}
