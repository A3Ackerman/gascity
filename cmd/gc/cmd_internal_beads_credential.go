package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/gastownhall/gascity/internal/credentialprovider"
	"github.com/spf13/cobra"
)

const hostedBeadsCredentialBridgeCommand = "gc internal beads-credential"

var hostedBeadsCredentialCache = credentialprovider.NewCache()

type hostedBeadsCredentialEnvelope struct {
	Token               string `json:"token"`
	ExpirationTimestamp string `json:"expirationTimestamp"`
}

func newInternalBeadsCredentialCmd(stdout, stderr io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:    "beads-credential",
		Short:  "Mint a hosted Beads credential",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			envelope, err := mintHostedBeadsCredential(cmd.Context())
			if err != nil {
				fmt.Fprintf(stderr, "gc internal beads-credential: %v\n", err) //nolint:errcheck
				return errExit
			}
			encoded, err := json.Marshal(envelope)
			if err != nil {
				fmt.Fprintln(stderr, "gc internal beads-credential: encoding credential") //nolint:errcheck
				return errExit
			}
			if _, err := fmt.Fprintln(stdout, string(encoded)); err != nil {
				return err
			}
			return nil
		},
	}
}

func mintHostedBeadsCredential(ctx context.Context) (hostedBeadsCredentialEnvelope, error) {
	argv, err := registryCredentialProviderArgv()
	if err != nil {
		return hostedBeadsCredentialEnvelope{}, err
	}
	provider, err := credentialprovider.New(argv)
	if err != nil {
		return hostedBeadsCredentialEnvelope{}, err
	}
	credential, err := hostedBeadsCredentialCache.Mint(ctx, provider, credentialprovider.Request{
		Audience: "beads",
		// The bd credential-command protocol carries no operation intent, so the
		// bridge cannot request a narrower read-only or write-only credential.
		RequiredScopes: []string{"beads:read", "beads:write"},
	})
	if err != nil {
		return hostedBeadsCredentialEnvelope{}, err
	}
	return hostedBeadsCredentialEnvelope{
		Token:               credential.AccessToken,
		ExpirationTimestamp: credential.ExpiresAt.UTC().Format(time.RFC3339),
	}, nil
}
