// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"strings"

	"github.com/rillanai/rillan/internal/secretstore"
	"github.com/spf13/cobra"
)

type credentialInput struct {
	apiKey       string
	accessToken  string
	refreshToken string
	idToken      string
	issuer       string
	audience     string
}

func addCredentialFlags(cmd *cobra.Command, input *credentialInput) {
	cmd.Flags().StringVar(&input.apiKey, "api-key", "", "API key to store securely")
	cmd.Flags().StringVar(&input.accessToken, "access-token", "", "Access token to store securely")
	cmd.Flags().StringVar(&input.refreshToken, "refresh-token", "", "Refresh token to store securely")
	cmd.Flags().StringVar(&input.idToken, "id-token", "", "ID token to store securely")
	cmd.Flags().StringVar(&input.issuer, "issuer", "", "OIDC issuer bound to the stored session")
	cmd.Flags().StringVar(&input.audience, "audience", "", "OIDC audience bound to the stored session")
}

func credentialFromInput(authStrategy string, endpoint string, input credentialInput) (secretstore.Credential, error) {
	switch strings.ToLower(strings.TrimSpace(authStrategy)) {
	case "api_key":
		if strings.TrimSpace(input.apiKey) == "" {
			return secretstore.Credential{}, fmt.Errorf("--api-key is required for api_key auth")
		}
		return secretstore.Credential{Kind: "api_key", APIKey: strings.TrimSpace(input.apiKey), Endpoint: endpoint, AuthStrategy: "api_key"}, nil
	case "browser_oidc", "device_oidc":
		if strings.TrimSpace(input.accessToken) == "" {
			return secretstore.Credential{}, fmt.Errorf("--access-token is required for %s auth", authStrategy)
		}
		return secretstore.Credential{
			Kind:         "oidc",
			AccessToken:  strings.TrimSpace(input.accessToken),
			RefreshToken: strings.TrimSpace(input.refreshToken),
			IDToken:      strings.TrimSpace(input.idToken),
			Endpoint:     endpoint,
			AuthStrategy: strings.ToLower(strings.TrimSpace(authStrategy)),
			Issuer:       strings.TrimSpace(input.issuer),
			Audience:     strings.TrimSpace(input.audience),
		}, nil
	case "none":
		return secretstore.Credential{}, fmt.Errorf("auth strategy none does not support login")
	default:
		return secretstore.Credential{}, fmt.Errorf("unsupported auth strategy %q", authStrategy)
	}
}
