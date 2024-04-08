// Copyright 2024 The PipeCD Authors.
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

package planpreview

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/pipe-cd/pipecd/pkg/app/piped/deploysource"
	provider "github.com/pipe-cd/pipecd/pkg/app/piped/platformprovider/ecs"
	"github.com/pipe-cd/pipecd/pkg/diff"
	"github.com/pipe-cd/pipecd/pkg/model"
)

func (b *builder) ecsdiff(
	ctx context.Context,
	app *model.Application,
	targetDSP deploysource.Provider,
	lastCommit string,
	buf *bytes.Buffer,
) (*diffResult, error) {
	var (
		oldManifest, newManifest provider.ECSManifest
		err                      error
	)

	// [MEMO] (1) デプロイ予定のマニフェスト(=serviceDef+taskDef)を取得

	newManifest, err = b.loadECSManifest(ctx, *app, targetDSP)
	if err != nil {
		fmt.Fprintf(buf, "failed to load ecs manifest at the head commit (%v)\n", err)
		return nil, err
	}

	if lastCommit == "" {
		fmt.Fprintf(buf, "failed to find the commit of the last successful deployment")
		return nil, fmt.Errorf("cannot get the old manifest without the last successful deployment")
	}

	runningDSP := deploysource.NewProvider(
		b.workingDir,
		deploysource.NewGitSourceCloner(b.gitClient, b.repoCfg, "running", lastCommit),
		*app.GitPath,
		b.secretDecrypter,
	)

	// [MEMO] (2) デプロイ済のマニフェスト(=serviceDef+taskDef)を取得。　実際の現環境を見る訳ではないのがポイント
	oldManifest, err = b.loadECSManifest(ctx, *app, runningDSP)
	if err != nil {
		fmt.Fprintf(buf, "failed to load ecs manifest at the running commit (%v)\n", err)
		return nil, err
	}

	// [MEMO] (3) マニフェストの差分を取得（Diff関数は実装する）
	result, err := provider.Diff(
		oldManifest,
		newManifest,
		diff.WithEquateEmpty(),
		diff.WithCompareNumberAndNumericString(),
	)
	if err != nil {
		fmt.Fprintf(buf, "failed to compare manifests (%v)\n", err)
		return nil, err
	}

	// [MEMO] (4) Diffの結果を表示（CloudRunと対してかわらないはず）
	summary := fmt.Sprintf("%d changes were detected", len(result.Diff.Nodes()))
	if result.NoChange() {
		fmt.Fprintln(buf, "No changes were detected")
		return &diffResult{
			summary:  "No changes were detected",
			noChange: true,
		}, nil
	}

	details := result.Render(provider.DiffRenderOptions{
		UseDiffCommand: true,
	})
	fmt.Fprintf(buf, "--- Last Deploy\n+++ Head Commit\n\n%s\n", details)

	return &diffResult{
		summary: summary,
	}, nil

}

func (b *builder) loadECSManifest(ctx context.Context, app model.Application, dsp deploysource.Provider) (provider.ECSManifest, error) {
	commit := dsp.Revision()
	cache := provider.ECSManifestCache{
		AppID:  app.Id,
		Cache:  b.appManifestsCache,
		Logger: b.logger,
	}

	manifest, ok := cache.Get(commit)
	if ok {
		return manifest, nil
	}

	ds, err := dsp.Get(ctx, io.Discard)
	if err != nil {
		return provider.ECSManifest{}, err
	}

	appCfg := ds.ApplicationConfig.ECSApplicationSpec
	if appCfg == nil {
		return provider.ECSManifest{}, fmt.Errorf("malformed application configuration file")
	}

	manifest, err = provider.LoadECSManifest(ds.AppDir, appCfg.Input.TaskDefinitionFile, appCfg.Input.ServiceDefinitionFile)
	if err != nil {
		return provider.ECSManifest{}, err
	}

	cache.Put(commit, manifest)
	return manifest, nil
}
