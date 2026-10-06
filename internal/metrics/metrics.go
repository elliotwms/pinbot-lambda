// Package metrics records CloudWatch metrics using the embedded metric format (EMF). Each metric is written as a JSON
// log line, which CloudWatch Logs extracts into a metric, so recording one needs no API calls or extra permissions.
// See https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/CloudWatch_Embedded_Metric_Format_Specification.html
package metrics

import (
	"encoding/json"
	"io"
	"maps"
	"slices"
	"sync"
	"time"
)

const namespace = "Pinbot"

// Unit is a CloudWatch metric unit
type Unit string

const (
	UnitCount Unit = "Count"
)

// Metrics records metrics with a set of default dimensions, such as the stack
type Metrics struct {
	mu         sync.Mutex
	w          io.Writer
	dimensions map[string]string
	now        func() time.Time
}

// New returns Metrics which writes to w, adding dimensions to every metric
func New(w io.Writer, dimensions map[string]string) *Metrics {
	return &Metrics{w: w, dimensions: dimensions, now: time.Now}
}

// Record records a single metric. dimensions are added to the default dimensions, and become part of the metric's
// identity, so they must have a small number of possible values. properties are written to the log line but don't
// create metrics, so they can hold high-cardinality values such as IDs, which can be queried with Logs Insights.
func (m *Metrics) Record(name string, value float64, unit Unit, dimensions map[string]string, properties map[string]any) {
	if m == nil {
		return
	}

	dims := maps.Clone(m.dimensions)
	if dims == nil {
		dims = map[string]string{}
	}
	maps.Copy(dims, dimensions)

	line := map[string]any{}
	maps.Copy(line, properties)
	for k, v := range dims {
		line[k] = v
	}
	line[name] = value
	line["_aws"] = map[string]any{
		"Timestamp": m.now().UnixMilli(),
		"CloudWatchMetrics": []map[string]any{{
			"Namespace":  namespace,
			"Dimensions": [][]string{slices.Sorted(maps.Keys(dims))},
			"Metrics":    []map[string]any{{"Name": name, "Unit": unit}},
		}},
	}

	bs, err := json.Marshal(line)
	if err != nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	_, _ = m.w.Write(append(bs, '\n'))
}
