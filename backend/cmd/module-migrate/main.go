package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/Wei-Shaw/LightBridge/internal/modulemigration"

	_ "github.com/lib/pq"
	_ "modernc.org/sqlite"
)

func main() {
	var opts modulemigration.Options
	var timeout time.Duration
	flag.StringVar(&opts.SourceKind, "source-kind", modulemigration.SourceLightBridge, "legacy source kind: lightbridge or sub2api")
	flag.StringVar(&opts.SourceDriver, "source-driver", "postgres", "legacy source database/sql driver")
	flag.StringVar(&opts.SourceDSN, "source-dsn", "", "legacy source database DSN")
	flag.StringVar(&opts.TargetDriver, "target-driver", "postgres", "target module-based LightBridge database/sql driver")
	flag.StringVar(&opts.TargetDSN, "target-dsn", "", "target module-based LightBridge database DSN")
	flag.StringVar(&opts.OpenAIModulePackage, "openai-module-package", "", "path to lightbridge-provider-openai module package")
	flag.StringVar(&opts.OpenAIModulePublicKeyPath, "openai-module-public-key", "", "path to Ed25519 public key used to verify the OpenAI provider module package")
	flag.StringVar(&opts.ModuleDataDir, "module-data-dir", "data", "target LightBridge module data directory")
	flag.BoolVar(&opts.DryRun, "dry-run", false, "scan and report without writing target database or installing modules")
	flag.BoolVar(&opts.InstallOpenAIModule, "install-openai-module", true, "install the OpenAI provider module into the target instance")
	flag.BoolVar(&opts.EnableOpenAIModule, "enable-openai-module", true, "mark the OpenAI provider module enabled after install and permission approval")
	flag.DurationVar(&timeout, "timeout", 10*time.Minute, "migration timeout")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	report, err := modulemigration.Run(ctx, opts)
	if err != nil {
		log.Fatalf("module migration failed: %v", err)
	}
	content, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		log.Fatalf("marshal migration report: %v", err)
	}
	_, _ = fmt.Fprintln(os.Stdout, string(content))
}
