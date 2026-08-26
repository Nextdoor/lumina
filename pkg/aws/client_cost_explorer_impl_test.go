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
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeCostExplorerAPI struct {
	output *costexplorer.GetSavingsPlansUtilizationOutput
	err    error
	input  *costexplorer.GetSavingsPlansUtilizationInput
}

func (f *fakeCostExplorerAPI) GetSavingsPlansUtilization(
	_ context.Context,
	input *costexplorer.GetSavingsPlansUtilizationInput,
	_ ...func(*costexplorer.Options),
) (*costexplorer.GetSavingsPlansUtilizationOutput, error) {
	f.input = input
	return f.output, f.err
}

func TestGetComputeSavingsPlansUnusedCommitment(t *testing.T) {
	tests := []struct {
		name    string
		output  *costexplorer.GetSavingsPlansUtilizationOutput
		err     error
		want    float64
		wantErr string
	}{
		{
			name: "returns latest hourly unused commitment",
			output: &costexplorer.GetSavingsPlansUtilizationOutput{
				SavingsPlansUtilizationsByTime: []types.SavingsPlansUtilizationByTime{
					{
						TimePeriod:  &types.DateInterval{End: sdkaws.String("2026-08-25T23:00:00Z")},
						Utilization: &types.SavingsPlansUtilization{UnusedCommitment: sdkaws.String("2")},
					},
					{
						TimePeriod:  &types.DateInterval{End: sdkaws.String("2026-08-25T22:00:00Z")},
						Utilization: &types.SavingsPlansUtilization{UnusedCommitment: sdkaws.String("3")},
					},
				},
			},
			want: 2,
		},
		{name: "propagates API error", err: errors.New("unavailable"), wantErr: "unavailable"},
		{
			name:    "rejects missing utilization",
			output:  &costexplorer.GetSavingsPlansUtilizationOutput{},
			wantErr: "did not include",
		},
		{
			name: "rejects invalid timestamp",
			output: &costexplorer.GetSavingsPlansUtilizationOutput{
				SavingsPlansUtilizationsByTime: []types.SavingsPlansUtilizationByTime{
					{
						TimePeriod:  &types.DateInterval{End: sdkaws.String("invalid")},
						Utilization: &types.SavingsPlansUtilization{UnusedCommitment: sdkaws.String("1")},
					},
				},
			},
			wantErr: "parse Cost Explorer time",
		},
		{
			name:    "rejects negative unused commitment",
			output:  utilizationOutput("2026-08-25T23:00:00Z", "-1"),
			wantErr: "invalid unused",
		},
		{
			name:    "rejects NaN unused commitment",
			output:  utilizationOutput("2026-08-25T23:00:00Z", "NaN"),
			wantErr: "invalid unused",
		},
		{
			name:    "rejects infinite unused commitment",
			output:  utilizationOutput("2026-08-25T23:00:00Z", "+Inf"),
			wantErr: "invalid unused",
		},
		{
			name:    "rejects future timestamp",
			output:  utilizationOutput("2026-08-27T00:00:00Z", "1"),
			wantErr: "ends in the future",
		},
		{
			name: "rejects invalid number",
			output: &costexplorer.GetSavingsPlansUtilizationOutput{
				SavingsPlansUtilizationsByTime: []types.SavingsPlansUtilizationByTime{
					{
						TimePeriod:  &types.DateInterval{End: sdkaws.String("2026-08-25T23:00:00Z")},
						Utilization: &types.SavingsPlansUtilization{UnusedCommitment: sdkaws.String("invalid")},
					},
				},
			},
			wantErr: "parse unused",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := &fakeCostExplorerAPI{output: tt.output, err: tt.err}
			client := &RealCostExplorerClient{client: api}
			got, err := client.GetComputeSavingsPlansUnusedCommitment(
				context.Background(), time.Date(2026, 8, 26, 15, 30, 0, 0, time.UTC),
			)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got.UnusedCommitment)
			assert.Equal(t, time.Date(2026, 8, 25, 23, 0, 0, 0, time.UTC), got.PeriodEnd)
			assert.Equal(t, "2026-08-19T00:00:00Z", *api.input.TimePeriod.Start)
			assert.Equal(t, "2026-08-26T00:00:00Z", *api.input.TimePeriod.End)
			assert.Equal(t, types.GranularityHourly, api.input.Granularity)
			assert.Equal(t, types.DimensionSavingsPlansType, api.input.Filter.Dimensions.Key)
			assert.Equal(t, []string{"ComputeSavingsPlans"}, api.input.Filter.Dimensions.Values)
		})
	}
}

func TestNewRealCostExplorerClient(t *testing.T) {
	creds := credentials.StaticCredentialsProvider{Value: sdkaws.Credentials{
		AccessKeyID: "test", SecretAccessKey: "test",
	}}
	client, err := NewRealCostExplorerClient(context.Background(), creds, "")
	require.NoError(t, err)
	require.NotNil(t, client)
	require.NotNil(t, client.client)
}

func TestNewRealCostExplorerClientWithEndpoint(t *testing.T) {
	creds := credentials.StaticCredentialsProvider{Value: sdkaws.Credentials{
		AccessKeyID: "test", SecretAccessKey: "test",
	}}
	client, err := NewRealCostExplorerClient(context.Background(), creds, "http://localhost:4566")
	require.NoError(t, err)
	require.NotNil(t, client)
}

func utilizationOutput(periodEnd, unused string) *costexplorer.GetSavingsPlansUtilizationOutput {
	return &costexplorer.GetSavingsPlansUtilizationOutput{
		SavingsPlansUtilizationsByTime: []types.SavingsPlansUtilizationByTime{{
			TimePeriod:  &types.DateInterval{End: sdkaws.String(periodEnd)},
			Utilization: &types.SavingsPlansUtilization{UnusedCommitment: sdkaws.String(unused)},
		}},
	}
}

func TestRealCostExplorerClientSerializesHourlyRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "AWSInsightsIndexService.GetSavingsPlansUtilization", r.Header.Get("X-Amz-Target"))
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.JSONEq(t, `{
			"TimePeriod": {
				"Start": "2026-08-19T00:00:00Z",
				"End": "2026-08-26T00:00:00Z"
			},
			"Granularity": "HOURLY",
			"Filter": {
				"Dimensions": {
					"Key": "SAVINGS_PLANS_TYPE",
					"Values": ["ComputeSavingsPlans"]
				}
			}
		}`, string(body))
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		_, err = w.Write([]byte(`{
			"SavingsPlansUtilizationsByTime": [{
				"TimePeriod": {"Start": "2026-08-24T23:00:00Z", "End": "2026-08-25T00:00:00Z"},
				"Utilization": {"UnusedCommitment": "0"}
			}]
		}`))
		require.NoError(t, err)
	}))
	defer server.Close()

	creds := credentials.StaticCredentialsProvider{Value: sdkaws.Credentials{
		AccessKeyID: "test", SecretAccessKey: "test",
	}}
	client, err := NewRealCostExplorerClient(context.Background(), creds, server.URL)
	require.NoError(t, err)
	observation, err := client.GetComputeSavingsPlansUnusedCommitment(
		context.Background(), time.Date(2026, 8, 26, 19, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	assert.Zero(t, observation.UnusedCommitment)
	assert.Equal(t, time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC), observation.PeriodEnd)
}
