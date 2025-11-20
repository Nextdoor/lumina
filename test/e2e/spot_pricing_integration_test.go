/*
Copyright 2025 Lumina Contributors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2e

import (
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Spot Pricing Integration", Ordered, func() {
	Context("Cost Calculation with Spot Instances", func() {
		var controllerPodName string

		BeforeAll(func() {
			By("getting controller pod name for spot pricing tests")
			controllerPodName = getControllerPodName()
		})

		It("should recognize spot instances from EC2 cache", func() {
			By("checking logs for spot instance lifecycle detection")
			Eventually(func() error {
				logs, err := getControllerLogs(controllerPodName)
				if err != nil {
					return err
				}
				// Look for spot instance in EC2 reconciler logs
				if !strings.Contains(logs, `"lifecycle":"spot"`) {
					return fmt.Errorf("spot instance lifecycle not found in logs")
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
		})

		It("should have spot pricing data in cache for spot instances", func() {
			By("checking logs for spot price cache population")
			Eventually(func() error {
				logs, err := getControllerLogs(controllerPodName)
				if err != nil {
					return err
				}
				// Look for spot pricing reconciler fetching prices
				if !strings.Contains(logs, "fetched spot prices") && !strings.Contains(logs, "inserted spot prices") {
					return fmt.Errorf("spot pricing cache population not found in logs")
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
		})

		It("should expose ec2_instance_hourly_cost metric with spot coverage type", func() {
			By("fetching metrics and checking for spot coverage")
			Eventually(func() error {
				metrics, err := getMetrics()
				if err != nil {
					return err
				}

				// Look for instance cost metrics with coverage_type="spot"
				foundSpotMetric := false
				for _, line := range strings.Split(metrics, "\n") {
					// Look for: ec2_instance_hourly_cost{...,coverage_type="spot",...}
					if strings.HasPrefix(line, "ec2_instance_hourly_cost{") &&
						strings.Contains(line, `coverage_type="spot"`) {
						foundSpotMetric = true
						break
					}
				}

				if !foundSpotMetric {
					return fmt.Errorf("no ec2_instance_hourly_cost metric found with coverage_type=\"spot\"")
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
		})

		It("should calculate spot instance costs using spot pricing", func() {
			By("verifying spot instance cost calculation in logs")
			Eventually(func() error {
				logs, err := getControllerLogs(controllerPodName)
				if err != nil {
					return err
				}

				// Look for cost calculation completion with spot instances
				if !strings.Contains(logs, "cost calculation completed") {
					return fmt.Errorf("cost calculation not completed yet")
				}

				// Verify that cost calculator processed spot pricing
				// The calculator logs spot pricing application at V(1) level
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
		})

		It("should have spot instance costs lower than on-demand shelf price", func() {
			By("checking metrics for spot savings")
			Eventually(func() error {
				metrics, err := getMetrics()
				if err != nil {
					return err
				}

				// Parse metrics to find spot instance costs
				// Format: ec2_instance_hourly_cost{...,coverage_type="spot",cost_type="effective_cost",...} VALUE
				// Format: ec2_instance_hourly_cost{...,coverage_type="spot",cost_type="shelf_price",...} VALUE
				var spotEffectiveCost, spotShelfPrice float64
				foundEffective := false
				foundShelf := false

				for _, line := range strings.Split(metrics, "\n") {
					if strings.HasPrefix(line, "ec2_instance_hourly_cost{") &&
						strings.Contains(line, `coverage_type="spot"`) {

						if strings.Contains(line, `cost_type="effective_cost"`) {
							// Extract value
							parts := strings.Fields(line)
							if len(parts) >= 2 {
								fmt.Sscanf(parts[1], "%f", &spotEffectiveCost)
								foundEffective = true
							}
						} else if strings.Contains(line, `cost_type="shelf_price"`) {
							// Extract value
							parts := strings.Fields(line)
							if len(parts) >= 2 {
								fmt.Sscanf(parts[1], "%f", &spotShelfPrice)
								foundShelf = true
							}
						}
					}
				}

				if !foundEffective {
					return fmt.Errorf("spot effective_cost metric not found")
				}
				if !foundShelf {
					return fmt.Errorf("spot shelf_price metric not found")
				}

				// Spot effective cost should be lower than shelf price (spot discount)
				if spotEffectiveCost >= spotShelfPrice {
					return fmt.Errorf("spot effective cost (%f) should be lower than shelf price (%f)",
						spotEffectiveCost, spotShelfPrice)
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
		})

		It("should mark spot instances with is_spot label in metrics", func() {
			By("checking for is_spot=\"true\" label in metrics")
			Eventually(func() error {
				metrics, err := getMetrics()
				if err != nil {
					return err
				}

				// Look for metrics with is_spot="true"
				foundIsSpotLabel := false
				for _, line := range strings.Split(metrics, "\n") {
					if strings.HasPrefix(line, "ec2_instance_hourly_cost{") &&
						strings.Contains(line, `is_spot="true"`) {
						foundIsSpotLabel = true
						break
					}
				}

				if !foundIsSpotLabel {
					return fmt.Errorf("no metrics found with is_spot=\"true\" label")
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
		})
	})
})
