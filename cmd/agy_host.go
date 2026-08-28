package cmd

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/spf13/cobra"

	"github.com/nextlevelbuilder/goclaw/internal/agyhost"
)

func agyHostCmd() *cobra.Command {
	var listen, agyPath, profilesDir, token string
	cmd := &cobra.Command{
		Use: "agy-host",
		Short: "Run the host-native Antigravity CLI bridge",
		RunE: func(_ *cobra.Command, _ []string) error {
			if token == "" { token = os.Getenv("GOCLAW_AGY_HOST_TOKEN") }
			bridge, err := agyhost.New(agyhost.Config{ListenAddr: listen, AGYPath: agyPath, ProfilesDir: profilesDir, Token: token})
			if err != nil { return err }
			slog.Info("starting AGY host bridge", "listen", listen, "profiles_dir", profilesDir)
			return http.ListenAndServe(listen, bridge.Handler())
		},
	}
	cmd.Flags().StringVar(&listen, "listen", "0.0.0.0:18891", "listen address")
	cmd.Flags().StringVar(&agyPath, "agy-path", "/root/.local/bin/agy", "path to agy executable")
	cmd.Flags().StringVar(&profilesDir, "profiles-dir", "/var/lib/goclaw-agy/profiles", "directory for independent AGY profiles")
	cmd.Flags().StringVar(&token, "token", "", "bridge bearer token (or GOCLAW_AGY_HOST_TOKEN)")
	cmd.Example = fmt.Sprintf("  GOCLAW_AGY_HOST_TOKEN=<secret> %s agy-host", "goclaw")
	return cmd
}
