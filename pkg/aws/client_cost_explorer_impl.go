// Copyright 2025 Nextdoor, Inc.
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

package aws

import (
	"context"
	"fmt"
	"strconv"
	"time"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
)

const costExplorerRegion = "us-east-1"

type costExplorerAPI interface {
	GetSavingsPlansUtilization(
		context.Context,
		*costexplorer.GetSavingsPlansUtilizationInput,
		...func(*costexplorer.Options),
	) (*costexplorer.GetSavingsPlansUtilizationOutput, error)
}

// RealCostExplorerClient retrieves settled billing data from AWS Cost Explorer.
type RealCostExplorerClient struct{ client costExplorerAPI }

func NewRealCostExplorerClient(
	ctx context.Context,
	creds sdkaws.CredentialsProvider,
	endpointURL string,
) (*RealCostExplorerClient, error) {
	cfg, err := awsconfig.LoadDefaultConfig(
		ctx,
		awsconfig.WithRegion(costExplorerRegion),
		awsconfig.WithCredentialsProvider(creds),
	)
	if err != nil {
		return nil, err
	}
	client := costexplorer.NewFromConfig(cfg, func(o *costexplorer.Options) {
		if endpointURL != "" {
			o.BaseEndpoint = &endpointURL
		}
	})
	return &RealCostExplorerClient{client: client}, nil
}

// GetComputeSavingsPlansUnusedCommitment returns the unused commitment from
// the latest available hourly bucket from recent fully completed UTC days.
func (c *RealCostExplorerClient) GetComputeSavingsPlansUnusedCommitment(
	ctx context.Context,
	now time.Time,
) (SavingsPlansUtilizationObservation, error) {
	end := now.UTC().Truncate(24 * time.Hour)
	// Hourly Cost Explorer data can arrive after the billing hour closes. Query
	// several complete days and use the latest bucket AWS has made available.
	start := end.AddDate(0, 0, -3)
	out, err := c.client.GetSavingsPlansUtilization(ctx, &costexplorer.GetSavingsPlansUtilizationInput{
		TimePeriod: &types.DateInterval{
			Start: sdkaws.String(start.Format(time.DateOnly)),
			End:   sdkaws.String(end.Format(time.DateOnly)),
		},
		Granularity: types.GranularityHourly,
		Filter: &types.Expression{
			Dimensions: &types.DimensionValues{
				Key:    types.DimensionSavingsPlansType,
				Values: []string{"Compute SP"},
			},
		},
	})
	if err != nil {
		return SavingsPlansUtilizationObservation{},
			fmt.Errorf("get Compute Savings Plans utilization: %w", err)
	}

	var latest SavingsPlansUtilizationObservation
	for _, period := range out.SavingsPlansUtilizationsByTime {
		if period.TimePeriod == nil || period.TimePeriod.End == nil ||
			period.Utilization == nil || period.Utilization.UnusedCommitment == nil {
			continue
		}
		periodEnd, err := parseCostExplorerTime(*period.TimePeriod.End)
		if err != nil {
			return SavingsPlansUtilizationObservation{}, err
		}
		unused, err := strconv.ParseFloat(*period.Utilization.UnusedCommitment, 64)
		if err != nil {
			return SavingsPlansUtilizationObservation{},
				fmt.Errorf("parse unused Compute Savings Plans commitment: %w", err)
		}
		if latest.PeriodEnd.IsZero() || periodEnd.After(latest.PeriodEnd) {
			latest = SavingsPlansUtilizationObservation{
				UnusedCommitment: unused,
				PeriodEnd:        periodEnd,
			}
		}
	}
	if latest.PeriodEnd.IsZero() {
		return SavingsPlansUtilizationObservation{},
			fmt.Errorf("compute Savings Plans utilization response did not include unused commitment")
	}
	return latest, nil
}

func parseCostExplorerTime(value string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339, time.DateOnly} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("parse Cost Explorer time %q", value)
}
