// Copyright 2022 The PipeCD Authors.
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

package insight

import (
	"context"

	"github.com/pipe-cd/pipecd/pkg/model"
)

type Provider interface {
	GetApplicationCounts(ctx context.Context, projectID string) (*ApplicationCounts, error)
	GetDeploymentFrequencyDataPoints(ctx context.Context, projectID, appID string, labels map[string]string, rangeFrom, rangeTo int64) ([]*model.InsightDataPoint, error)
	GetDeploymentChangeFailureRateDataPoints(ctx context.Context, projectID, appID string, labels map[string]string, rangeFrom, rangeTo int64) ([]*model.InsightDataPoint, error)
}

type provider struct {
	store Store
}

func NewProvider(s Store) Provider {
	return &provider{
		store: s,
	}
}

// TODO: Add cache layer.
func (p *provider) GetApplicationCounts(ctx context.Context, projectID string) (*ApplicationCounts, error) {
	data, err := p.store.GetApplications(ctx, projectID)
	if err != nil {
		return nil, err
	}

	counts := buildApplicationCounts(data)
	return &counts, nil
}

// TODO: Add cache layer.
func (p *provider) GetDeploymentFrequencyDataPoints(ctx context.Context, projectID, appID string, labels map[string]string, rangeFrom, rangeTo int64) ([]*model.InsightDataPoint, error) {
	ds, err := p.store.ListCompletedDeployments(ctx, projectID, rangeFrom, rangeTo)
	if err != nil {
		return nil, err
	}

	points := buildDeploymentFrequencyDataPoints(ds, appID, labels)
	return points, nil
}

// TODO: Add cache layer.
func (p *provider) GetDeploymentChangeFailureRateDataPoints(ctx context.Context, projectID, appID string, labels map[string]string, rangeFrom, rangeTo int64) ([]*model.InsightDataPoint, error) {
	ds, err := p.store.ListCompletedDeployments(ctx, projectID, rangeFrom, rangeTo)
	if err != nil {
		return nil, err
	}

	points := buildDeploymentChangeFailureRateDataPoints(ds, appID, labels)
	return points, nil
}

func buildDeploymentFrequencyDataPoints(ds []*DeploymentData, appID string, labels map[string]string) []*model.InsightDataPoint {
	ds = filterDeploymentData(ds, appID, labels)
	if len(ds) == 0 {
		return []*model.InsightDataPoint{}
	}

	var (
		out      = make([]*model.InsightDataPoint, 0)
		curPoint *model.InsightDataPoint
	)
	for _, d := range ds {
		if curPoint == nil || curPoint.Timestamp != d.CompletedAtDay {
			curPoint = &model.InsightDataPoint{
				Timestamp: d.CompletedAtDay,
			}
			out = append(out, curPoint)
		}
		curPoint.Value += 1
	}

	return out
}

func buildDeploymentChangeFailureRateDataPoints(ds []*DeploymentData, appID string, labels map[string]string) []*model.InsightDataPoint {
	ds = filterDeploymentData(ds, appID, labels)
	if len(ds) == 0 {
		return []*model.InsightDataPoint{}
	}

	var (
		out                              = make([]*model.InsightDataPoint, 0)
		curPoint *model.InsightDataPoint = nil
		curTotal                         = 0
		curFails                         = 0
	)
	for _, d := range ds {
		if curPoint == nil || curPoint.Timestamp != d.CompletedAtDay {
			if curPoint != nil {
				curPoint.Value = float32(curFails) / float32(curTotal)
				curTotal = 0
				curFails = 0
			}
			curPoint = &model.InsightDataPoint{
				Timestamp: d.CompletedAtDay,
			}
			out = append(out, curPoint)
		}
		curTotal += 1
		if d.CompleteStatus == model.DeploymentStatus_DEPLOYMENT_FAILURE.String() {
			curFails += 1
		}
	}
	if curPoint != nil {
		curPoint.Value = float32(curFails) / float32(curTotal)
	}

	return out
}

func filterDeploymentData(ds []*DeploymentData, appID string, labels map[string]string) []*DeploymentData {
	if appID == "" && len(labels) == 0 {
		return ds
	}

	targets := make([]*DeploymentData, 0, len(ds))
	for _, d := range ds {
		if appID != "" && d.AppID != appID {
			continue
		}
		// TODO: Support filtering by labels.
		targets = append(targets, d)
	}

	return targets
}

func buildApplicationCounts(d *ProjectApplicationData) ApplicationCounts {
	if len(d.Applications) == 0 {
		return ApplicationCounts{
			UpdatedAt: d.UpdatedAt,
		}
	}

	type key struct {
		status string
		kind   string
	}
	m := make(map[key]int, len(d.Applications))
	for _, app := range d.Applications {
		k := key{
			status: app.Status,
			kind:   app.Kind,
		}
		m[k]++
	}

	counts := make([]model.InsightApplicationCount, 0, len(m))
	for k, c := range m {
		counts = append(counts, model.InsightApplicationCount{
			Labels: map[string]string{
				model.InsightApplicationCountLabelKey_KIND.String():          k.kind,
				model.InsightApplicationCountLabelKey_ACTIVE_STATUS.String(): k.status,
			},
			Count: int32(c),
		})
	}

	return ApplicationCounts{
		Counts:    counts,
		UpdatedAt: d.UpdatedAt,
	}
}

func determineApplicationStatus(a *model.Application) model.ApplicationActiveStatus {
	if a.Deleted {
		return model.ApplicationActiveStatus_DELETED
	}
	if a.Disabled {
		return model.ApplicationActiveStatus_DISABLED
	}
	return model.ApplicationActiveStatus_ENABLED
}