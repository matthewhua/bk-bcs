/*
 * Tencent is pleased to support the open source community by making Blueking Container Service available.
 * Copyright (C) 2022 THL A29 Limited, a Tencent company. All rights reserved.
 * Licensed under the MIT License (the "License"); you may not use this file except
 * in compliance with the License. You may obtain a copy of the License at
 *
 * 	http://opensource.org/licenses/MIT
 *
 * Unless required by applicable law or agreed to in writing, software distributed under,
 * the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
 * either express or implied. See the License for the specific language governing permissions and
 * limitations under the License.
 */

package hpa

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Tencent/bk-bcs/bcs-services/cluster-resources/pkg/resource/form/model"
)

var lightHPAManifest = map[string]interface{}{
	"spec": map[string]interface{}{
		"scaleTargetRef": map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"name":       "deployment-xxx1",
		},
		"minReplicas": int64(3),
		"maxReplicas": int64(8),
		"metrics": []interface{}{
			map[string]interface{}{
				"resource": map[string]interface{}{
					"name": "cpu",
					"target": map[string]interface{}{
						"averageValue": "80",
						"type":         "AverageValue",
					},
				},
				"type": "Resource",
			},
			map[string]interface{}{
				"resource": map[string]interface{}{
					"name": "cpu",
					"target": map[string]interface{}{
						"averageUtilization": int64(50),
						"type":               "Utilization",
					},
				},
				"type": "Resource",
			},
			map[string]interface{}{
				"resource": map[string]interface{}{
					"name": "memory",
					"target": map[string]interface{}{
						"averageValue": "60",
						"type":         "AverageValue",
					},
				},
				"type": "Resource",
			},
			map[string]interface{}{
				"external": map[string]interface{}{
					"metric": map[string]interface{}{
						"name": "ext1",
						"selector": map[string]interface{}{
							"matchExpressions": []interface{}{
								map[string]interface{}{
									"key":      "exp1",
									"operator": "In",
									"values": []interface{}{
										"value1",
									},
								},
								map[string]interface{}{
									"key":      "exp2",
									"operator": "NotIn",
									"values": []interface{}{
										"value2",
										"value2-1",
									},
								},
								map[string]interface{}{
									"key":      "exp3",
									"operator": "Exists",
								},
							},
							"matchLabels": map[string]interface{}{
								"key1": "val1",
								"key2": "val2",
							},
						},
					},
					"target": map[string]interface{}{
						"type":  "Value",
						"value": "10",
					},
				},
				"type": "External",
			},
			map[string]interface{}{
				"object": map[string]interface{}{
					"describedObject": map[string]interface{}{
						"apiVersion": "apps/v1",
						"kind":       "Deployment",
						"name":       "deploy-aaa",
					},
					"metric": map[string]interface{}{
						"name": "object1",
						"selector": map[string]interface{}{
							"matchExpressions": []interface{}{
								map[string]interface{}{
									"key":      "exp1",
									"operator": "In",
									"values": []interface{}{
										"val1",
										"val2",
									},
								},
								map[string]interface{}{
									"key":      "exp2",
									"operator": "Exists",
								},
							},
							"matchLabels": map[string]interface{}{
								"key1": "val1",
							},
						},
					},
					"target": map[string]interface{}{
						"averageValue": "10",
						"type":         "AverageValue",
					},
				},
				"type": "Object",
			},
			map[string]interface{}{
				"object": map[string]interface{}{
					"describedObject": map[string]interface{}{
						"apiVersion": "tkex.tencent.com/v1alpha1",
						"kind":       "GameDeployment",
						"name":       "gdeploy-xx",
					},
					"metric": map[string]interface{}{
						"name": "object2",
						"selector": map[string]interface{}{
							"matchExpressions": []interface{}{
								map[string]interface{}{
									"key":      "exp1",
									"operator": "NotIn",
									"values": []interface{}{
										"val1",
										"val2",
									},
								},
							},
							"matchLabels": map[string]interface{}{
								"key2": "val2",
							},
						},
					},
					"target": map[string]interface{}{
						"type":  "Value",
						"value": "20",
					},
				},
				"type": "Object",
			},
			map[string]interface{}{
				"pods": map[string]interface{}{
					"metric": map[string]interface{}{
						"name": "pod1",
						"selector": map[string]interface{}{
							"matchExpressions": []interface{}{
								map[string]interface{}{
									"key":      "exp1",
									"operator": "Exists",
								},
								map[string]interface{}{
									"key":      "exp2",
									"operator": "In",
									"values": []interface{}{
										"val1",
										"val2",
									},
								},
							},
							"matchLabels": map[string]interface{}{
								"key11": "val22",
							},
						},
					},
					"target": map[string]interface{}{
						"averageValue": "10",
						"type":         "AverageValue",
					},
				},
				"type": "Pods",
			},
		},
	},
}

var exceptedHPASpec = model.HPASpec{
	Ref: model.HPATargetRef{
		APIVersion:  "apps/v1",
		Kind:        "Deployment",
		ResName:     "deployment-xxx1",
		MinReplicas: int64(3),
		MaxReplicas: int64(8),
	},
	Resource: model.ResourceMetric{
		Items: []model.ResourceMetricItem{
			{
				Name:  "cpu",
				Type:  HPATargetTypeAverageValue,
				Value: "80",
			},
			{
				Name:  "cpu",
				Type:  HPATargetTypeUtilization,
				Value: "50",
			},
			{
				Name:  "memory",
				Type:  HPATargetTypeAverageValue,
				Value: "60",
			},
		},
	},
	External: model.ExternalMetric{
		Items: []model.ExternalMetricItem{
			{
				Name:  "ext1",
				Type:  HPATargetTypeValue,
				Value: "10",
				Selector: model.MetricSelector{
					Expressions: []model.ExpSelector{
						{
							Key:    "exp1",
							Op:     "In",
							Values: "value1",
						},
						{
							Key:    "exp2",
							Op:     "NotIn",
							Values: "value2,value2-1",
						},
						{
							Key: "exp3",
							Op:  "Exists",
						},
					},
					Labels: []model.LabelSelector{
						{
							Key:   "key1",
							Value: "val1",
						},
						{
							Key:   "key2",
							Value: "val2",
						},
					},
				},
			},
		},
	},
	Object: model.ObjectMetric{
		Items: []model.ObjectMetricItem{
			{
				Name:       "object1",
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				ResName:    "deploy-aaa",
				Type:       HPATargetTypeAverageValue,
				Value:      "10",
				Selector: model.MetricSelector{
					Expressions: []model.ExpSelector{
						{
							Key:    "exp1",
							Op:     "In",
							Values: "val1,val2",
						},
						{
							Key: "exp2",
							Op:  "Exists",
						},
					},
					Labels: []model.LabelSelector{
						{
							Key:   "key1",
							Value: "val1",
						},
					},
				},
			},
			{
				Name:       "object2",
				APIVersion: "tkex.tencent.com/v1alpha1",
				Kind:       "GameDeployment",
				ResName:    "gdeploy-xx",
				Type:       HPATargetTypeValue,
				Value:      "20",
				Selector: model.MetricSelector{
					Expressions: []model.ExpSelector{
						{
							Key:    "exp1",
							Op:     "NotIn",
							Values: "val1,val2",
						},
					},
					Labels: []model.LabelSelector{
						{
							Key:   "key2",
							Value: "val2",
						},
					},
				},
			},
		},
	},
	Pod: model.PodMetric{
		Items: []model.PodMetricItem{
			{
				Name:  "pod1",
				Type:  HPATargetTypeAverageValue,
				Value: "10",
				Selector: model.MetricSelector{
					Expressions: []model.ExpSelector{
						{
							Key: "exp1",
							Op:  "Exists",
						},
						{
							Key:    "exp2",
							Op:     "In",
							Values: "val1,val2",
						},
					},
					Labels: []model.LabelSelector{
						{
							Key:   "key11",
							Value: "val22",
						},
					},
				},
			},
		},
	},
}

func TestParseHPASpec(t *testing.T) {
	actualHPASpec := model.HPASpec{}
	ParseHPASpec(lightHPAManifest, &actualHPASpec)
	assert.Equal(t, exceptedHPASpec, actualHPASpec)
}
