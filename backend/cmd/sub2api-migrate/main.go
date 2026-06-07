package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/Wei-Shaw/LightBridge/internal/modulemigration"

	_ "github.com/lib/pq"
	_ "modernc.org/sqlite"
)

func main() {
	var (
		sourceDriver        string
		sourceDSN           string
		targetDriver        string
		targetDSN           string
		moduleDataDir       string
		registryURL         string
		openAIModulePackage string
		openAIModulePubKey  string
		timeout             time.Duration
		dryRun              bool
		skipModuleInstall   bool
	)

	flag.StringVar(&sourceDriver, "source-driver", "postgres", "legacy Sub2API database/sql driver")
	flag.StringVar(&sourceDSN, "source-dsn", "", "legacy Sub2API database DSN")
	flag.StringVar(&targetDriver, "target-driver", "postgres", "target LightBridge database/sql driver")
	flag.StringVar(&targetDSN, "target-dsn", "", "target LightBridge database DSN")
	flag.StringVar(&moduleDataDir, "module-data-dir", "data", "target LightBridge module data directory")
	flag.StringVar(&registryURL, "module-registry-url", modulemigration.DefaultModuleMigrationRegistryURL, "module registry URL used to fetch the OpenAI Provider package")
	flag.StringVar(&openAIModulePackage, "openai-module-package", "", "optional local OpenAI Provider module package path; skips registry download when set")
	flag.StringVar(&openAIModulePubKey, "openai-module-public-key", "", "optional Ed25519 public key path for OpenAI Provider package verification")
	flag.BoolVar(&dryRun, "dry-run", false, "scan and report without writing target database or installing modules")
	flag.BoolVar(&skipModuleInstall, "skip-openai-module-install", false, "migrate data without installing the OpenAI Provider module")
	flag.DurationVar(&timeout, "timeout", 20*time.Minute, "migration timeout")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if strings.TrimSpace(sourceDSN) == "" || strings.TrimSpace(targetDSN) == "" {
		log.Fatal("source-dsn and target-dsn are required")
	}

	cleanup := func() {}
	if !dryRun && !skipModuleInstall && strings.TrimSpace(openAIModulePackage) == "" {
		resolved, err := modulemigration.ResolveOpenAIModulePackage(ctx, registryURL, 20, openAIModulePubKey)
		if err != nil {
			log.Fatalf("resolve OpenAI Provider module package: %v", err)
		}
		openAIModulePackage = resolved.PackagePath
		openAIModulePubKey = resolved.PublicKeyPath
		cleanup = func() { _ = os.RemoveAll(resolved.Workspace) }
	}
	defer cleanup()

	opts := modulemigration.Options{
		SourceKind:                modulemigration.SourceSub2API,
		SourceDriver:              sourceDriver,
		SourceDSN:                 sourceDSN,
		TargetDriver:              targetDriver,
		TargetDSN:                 targetDSN,
		OpenAIModulePackage:       openAIModulePackage,
		OpenAIModulePublicKeyPath: openAIModulePubKey,
		ModuleDataDir:             moduleDataDir,
		DryRun:                    dryRun,
		InstallOpenAIModule:       !skipModuleInstall,
		EnableOpenAIModule:        true,
	}

	report, err := modulemigration.Run(ctx, opts)
	if err != nil {
		log.Fatalf("sub2api production migration failed: %v", err)
	}
	content, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		log.Fatalf("marshal migration report: %v", err)
	}
	_, _ = fmt.Fprintln(os.Stdout, string(content))
}
