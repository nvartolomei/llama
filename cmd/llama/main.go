// Copyright 2020 Nelson Elhage
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"flag"
	"log"
	"os"
	"runtime/trace"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/google/subcommands"
	"github.com/nelhage/llama/cmd/internal/cli"
	"github.com/nelhage/llama/store"
	"github.com/nelhage/llama/store/s3store"
)

func main() {
	subcommands.Register(subcommands.HelpCommand(), "")

	subcommands.Register(&InvokeCommand{}, "")
	subcommands.Register(&XargsCommand{}, "")
	subcommands.Register(&DaemonCommand{}, "")

	subcommands.Register(&StoreCommand{}, "internals")
	subcommands.Register(&GetCommand{}, "internals")

	ctx := context.Background()
	code := runLlama(ctx)
	os.Exit(code)
}

func runLlama(ctx context.Context) int {
	var state cli.GlobalState
	var regionOverride string
	var storeOverride string
	debugAWS := false
	var traceFile string
	var storeConcurrency int
	flag.StringVar(&regionOverride, "region", "", "S3 region for commands")
	flag.StringVar(&storeOverride, "store", "", "Path to the llama object store. s3://BUCKET/PATH")
	flag.BoolVar(&debugAWS, "debug-aws", false, "Log all AWS requests/responses")
	flag.StringVar(&traceFile, "trace", "", "Log trace to file")
	flag.IntVar(&storeConcurrency, "s3-concurrency", 8, "Maximum concurrent S3 uploads/downloads")

	flag.Parse()

	cfg, err := cli.ReadConfig(cli.ConfigPath())
	if err != nil {
		log.Fatalf("reading config file: %s", err.Error())
	}

	if storeOverride == "" {
		storeOverride = os.Getenv("LLAMA_OBJECT_STORE")
	}
	if storeOverride != "" {
		cfg.Store = storeOverride
	}
	if traceFile != "" {
		f, err := os.Create(traceFile)
		if err != nil {
			log.Fatalf("open trace: %s", err.Error())
		}
		defer f.Close()
		trace.Start(f)
		defer trace.Stop()
	}

	ctx, task := trace.NewTask(ctx, "llama")
	defer task.End()

	trace.WithRegion(ctx, "global-init", func() {
		awscfg := aws.NewConfig()
		if regionOverride != "" {
			awscfg = awscfg.WithRegion(regionOverride)
		} else if cfg.Region != "" {
			awscfg = awscfg.WithRegion(cfg.Region)
		}
		if debugAWS {
			awscfg = awscfg.WithLogLevel(aws.LogDebugWithHTTPBody)
		}
		state.Session = session.Must(session.NewSession(awscfg))
		state.Store, err = s3store.FromSession(state.Session, cfg.Store)
		if storeConcurrency > 0 && err == nil {
			state.Store = store.LimitConcurrency(state.Store, storeConcurrency)
		}

		ctx = cli.WithState(ctx, &state)
	})
	if err != nil {
		log.Fatal(err.Error())
	}

	return int(subcommands.Execute(ctx))
}
