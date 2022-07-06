// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package otlphttpexporter

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/config"
	"go.opentelemetry.io/collector/config/confighttp"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/consumer/consumererror"
	"go.opentelemetry.io/collector/consumer/consumertest"
	"go.opentelemetry.io/collector/exporter/exporterhelper"
	"go.opentelemetry.io/collector/internal/testdata"
	"go.opentelemetry.io/collector/internal/testutil"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"
	"go.opentelemetry.io/collector/receiver/otlpreceiver"
)

func TestInvalidConfig(t *testing.T) {
	config := &Config{
		HTTPClientSettings: confighttp.HTTPClientSettings{
			Endpoint: "",
		},
	}
	f := NewFactory()
	set := componenttest.NewNopExporterCreateSettings()
	_, err := f.CreateTracesExporter(context.Background(), set, config)
	require.Error(t, err)
	_, err = f.CreateMetricsExporter(context.Background(), set, config)
	require.Error(t, err)
	_, err = f.CreateLogsExporter(context.Background(), set, config)
	require.Error(t, err)
}

func TestTraceNoBackend(t *testing.T) {
	addr := testutil.GetAvailableLocalAddress(t)
	exp := startTracesExporter(t, "", fmt.Sprintf("http://%s/v1/traces", addr))
	td := testdata.GenerateTraces(1)
	assert.Error(t, exp.ConsumeTraces(context.Background(), td))
}

func TestTraceInvalidUrl(t *testing.T) {
	exp := startTracesExporter(t, "http:/\\//this_is_an/*/invalid_url", "")
	td := testdata.GenerateTraces(1)
	assert.Error(t, exp.ConsumeTraces(context.Background(), td))

	exp = startTracesExporter(t, "", "http:/\\//this_is_an/*/invalid_url")
	td = testdata.GenerateTraces(1)
	assert.Error(t, exp.ConsumeTraces(context.Background(), td))
}

func TestTraceError(t *testing.T) {
	addr := testutil.GetAvailableLocalAddress(t)

	startTracesReceiver(t, addr, consumertest.NewErr(errors.New("my_error")))
	exp := startTracesExporter(t, "", fmt.Sprintf("http://%s/v1/traces", addr))

	td := testdata.GenerateTraces(1)
	assert.Error(t, exp.ConsumeTraces(context.Background(), td))
}

func TestTraceRoundTrip(t *testing.T) {
	addr := testutil.GetAvailableLocalAddress(t)

	tests := []struct {
		name        string
		baseURL     string
		overrideURL string
	}{
		{
			name:        "wrongbase",
			baseURL:     "http://wronghostname",
			overrideURL: fmt.Sprintf("http://%s/v1/traces", addr),
		},
		{
			name:        "onlybase",
			baseURL:     fmt.Sprintf("http://%s", addr),
			overrideURL: "",
		},
		{
			name:        "override",
			baseURL:     "",
			overrideURL: fmt.Sprintf("http://%s/v1/traces", addr),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sink := new(consumertest.TracesSink)
			startTracesReceiver(t, addr, sink)
			exp := startTracesExporter(t, test.baseURL, test.overrideURL)

			td := testdata.GenerateTraces(1)
			assert.NoError(t, exp.ConsumeTraces(context.Background(), td))
			require.Eventually(t, func() bool {
				return sink.SpanCount() > 0
			}, 1*time.Second, 10*time.Millisecond)
			allTraces := sink.AllTraces()
			require.Len(t, allTraces, 1)
			assert.EqualValues(t, td, allTraces[0])
		})
	}
}

func TestMetricsError(t *testing.T) {
	addr := testutil.GetAvailableLocalAddress(t)

	startMetricsReceiver(t, addr, consumertest.NewErr(errors.New("my_error")))
	exp := startMetricsExporter(t, "", fmt.Sprintf("http://%s/v1/metrics", addr))

	md := testdata.GenerateMetrics(1)
	assert.Error(t, exp.ConsumeMetrics(context.Background(), md))
}

func TestMetricsRoundTrip(t *testing.T) {
	addr := testutil.GetAvailableLocalAddress(t)

	tests := []struct {
		name        string
		baseURL     string
		overrideURL string
	}{
		{
			name:        "wrongbase",
			baseURL:     "http://wronghostname",
			overrideURL: fmt.Sprintf("http://%s/v1/metrics", addr),
		},
		{
			name:        "onlybase",
			baseURL:     fmt.Sprintf("http://%s", addr),
			overrideURL: "",
		},
		{
			name:        "override",
			baseURL:     "",
			overrideURL: fmt.Sprintf("http://%s/v1/metrics", addr),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sink := new(consumertest.MetricsSink)
			startMetricsReceiver(t, addr, sink)
			exp := startMetricsExporter(t, test.baseURL, test.overrideURL)

			md := testdata.GenerateMetrics(1)
			assert.NoError(t, exp.ConsumeMetrics(context.Background(), md))
			require.Eventually(t, func() bool {
				return sink.DataPointCount() > 0
			}, 1*time.Second, 10*time.Millisecond)
			allMetrics := sink.AllMetrics()
			require.Len(t, allMetrics, 1)
			assert.EqualValues(t, md, allMetrics[0])
		})
	}
}

func TestLogsError(t *testing.T) {
	addr := testutil.GetAvailableLocalAddress(t)

	startLogsReceiver(t, addr, consumertest.NewErr(errors.New("my_error")))
	exp := startLogsExporter(t, "", fmt.Sprintf("http://%s/v1/logs", addr))

	md := testdata.GenerateLogs(1)
	assert.Error(t, exp.ConsumeLogs(context.Background(), md))
}

func TestLogsRoundTrip(t *testing.T) {
	addr := testutil.GetAvailableLocalAddress(t)

	tests := []struct {
		name        string
		baseURL     string
		overrideURL string
	}{
		{
			name:        "wrongbase",
			baseURL:     "http://wronghostname",
			overrideURL: fmt.Sprintf("http://%s/v1/logs", addr),
		},
		{
			name:        "onlybase",
			baseURL:     fmt.Sprintf("http://%s", addr),
			overrideURL: "",
		},
		{
			name:        "override",
			baseURL:     "",
			overrideURL: fmt.Sprintf("http://%s/v1/logs", addr),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sink := new(consumertest.LogsSink)
			startLogsReceiver(t, addr, sink)
			exp := startLogsExporter(t, test.baseURL, test.overrideURL)

			md := testdata.GenerateLogs(1)
			assert.NoError(t, exp.ConsumeLogs(context.Background(), md))
			require.Eventually(t, func() bool {
				return sink.LogRecordCount() > 0
			}, 1*time.Second, 10*time.Millisecond)
			allLogs := sink.AllLogs()
			require.Len(t, allLogs, 1)
			assert.EqualValues(t, md, allLogs[0])
		})
	}
}

func TestIssue_4221(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { assert.NoError(t, r.Body.Close()) }()
		compressedData, err := ioutil.ReadAll(r.Body)
		require.NoError(t, err)
		gzipReader, err := gzip.NewReader(bytes.NewReader(compressedData))
		require.NoError(t, err)
		data, err := ioutil.ReadAll(gzipReader)
		require.NoError(t, err)
		base64Data := base64.StdEncoding.EncodeToString(data)
		// Verify same base64 encoded string is received.
		assert.Equal(t, "CscBCkkKIAoMc2VydmljZS5uYW1lEhAKDnVvcC5zdGFnZS1ldS0xCiUKGW91dHN5c3RlbXMubW9kdWxlLnZlcnNpb24SCAoGOTAzMzg2EnoKEQoMdW9wX2NhbmFyaWVzEgExEmUKEEMDhT8Ib0+Mhs8Zi2VR34QSCOVRPDJ5XEG5IgA5QE41aASRrxZBQE41aASRrxZKEAoKc3Bhbl9pbmRleBICGANKHwoNY29kZS5mdW5jdGlvbhIOCgxteUZ1bmN0aW9uMzZ6AA==", base64Data)
		unbase64Data, err := base64.StdEncoding.DecodeString(base64Data)
		require.NoError(t, err)
		tr := ptraceotlp.NewRequest()
		require.NoError(t, tr.UnmarshalProto(unbase64Data))
		span := tr.Traces().ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(0)
		assert.Equal(t, "4303853f086f4f8c86cf198b6551df84", span.TraceID().HexString())
		assert.Equal(t, "e5513c32795c41b9", span.SpanID().HexString())
	}))

	exp := startTracesExporter(t, "", svr.URL)

	md := ptrace.NewTraces()
	rms := md.ResourceSpans().AppendEmpty()
	rms.Resource().Attributes().UpsertString("service.name", "uop.stage-eu-1")
	rms.Resource().Attributes().UpsertString("outsystems.module.version", "903386")
	ils := rms.ScopeSpans().AppendEmpty()
	ils.Scope().SetName("uop_canaries")
	ils.Scope().SetVersion("1")
	span := ils.Spans().AppendEmpty()

	var traceIDBytes [16]byte
	traceIDBytesSlice, err := hex.DecodeString("4303853f086f4f8c86cf198b6551df84")
	require.NoError(t, err)
	copy(traceIDBytes[:], traceIDBytesSlice)
	span.SetTraceID(pcommon.NewTraceID(traceIDBytes))
	assert.Equal(t, "4303853f086f4f8c86cf198b6551df84", span.TraceID().HexString())

	var spanIDBytes [8]byte
	spanIDBytesSlice, err := hex.DecodeString("e5513c32795c41b9")
	require.NoError(t, err)
	copy(spanIDBytes[:], spanIDBytesSlice)
	span.SetSpanID(pcommon.NewSpanID(spanIDBytes))
	assert.Equal(t, "e5513c32795c41b9", span.SpanID().HexString())

	span.SetEndTimestamp(1634684637873000000)
	span.Attributes().UpsertInt("span_index", 3)
	span.Attributes().UpsertString("code.function", "myFunction36")
	span.SetStartTimestamp(1634684637873000000)

	assert.NoError(t, exp.ConsumeTraces(context.Background(), md))
}

func startTracesExporter(t *testing.T, baseURL string, overrideURL string) component.TracesExporter {
	factory := NewFactory()
	cfg := createExporterConfig(baseURL, factory.CreateDefaultConfig())
	cfg.TracesEndpoint = overrideURL
	exp, err := factory.CreateTracesExporter(context.Background(), componenttest.NewNopExporterCreateSettings(), cfg)
	require.NoError(t, err)
	startAndCleanup(t, exp)
	return exp
}

func startMetricsExporter(t *testing.T, baseURL string, overrideURL string) component.MetricsExporter {
	factory := NewFactory()
	cfg := createExporterConfig(baseURL, factory.CreateDefaultConfig())
	cfg.MetricsEndpoint = overrideURL
	exp, err := factory.CreateMetricsExporter(context.Background(), componenttest.NewNopExporterCreateSettings(), cfg)
	require.NoError(t, err)
	startAndCleanup(t, exp)
	return exp
}

func startLogsExporter(t *testing.T, baseURL string, overrideURL string) component.LogsExporter {
	factory := NewFactory()
	cfg := createExporterConfig(baseURL, factory.CreateDefaultConfig())
	cfg.LogsEndpoint = overrideURL
	exp, err := factory.CreateLogsExporter(context.Background(), componenttest.NewNopExporterCreateSettings(), cfg)
	require.NoError(t, err)
	startAndCleanup(t, exp)
	return exp
}

func createExporterConfig(baseURL string, defaultCfg config.Exporter) *Config {
	cfg := defaultCfg.(*Config)
	cfg.Endpoint = baseURL
	cfg.QueueSettings.Enabled = false
	cfg.RetrySettings.Enabled = false
	return cfg
}

func startTracesReceiver(t *testing.T, addr string, next consumer.Traces) {
	factory := otlpreceiver.NewFactory()
	cfg := createReceiverConfig(addr, factory.CreateDefaultConfig())
	recv, err := factory.CreateTracesReceiver(context.Background(), componenttest.NewNopReceiverCreateSettings(), cfg, next)
	require.NoError(t, err)
	startAndCleanup(t, recv)
}

func startMetricsReceiver(t *testing.T, addr string, next consumer.Metrics) {
	factory := otlpreceiver.NewFactory()
	cfg := createReceiverConfig(addr, factory.CreateDefaultConfig())
	recv, err := factory.CreateMetricsReceiver(context.Background(), componenttest.NewNopReceiverCreateSettings(), cfg, next)
	require.NoError(t, err)
	startAndCleanup(t, recv)
}

func startLogsReceiver(t *testing.T, addr string, next consumer.Logs) {
	factory := otlpreceiver.NewFactory()
	cfg := createReceiverConfig(addr, factory.CreateDefaultConfig())
	recv, err := factory.CreateLogsReceiver(context.Background(), componenttest.NewNopReceiverCreateSettings(), cfg, next)
	require.NoError(t, err)
	startAndCleanup(t, recv)
}

func createReceiverConfig(addr string, defaultCfg config.Receiver) *otlpreceiver.Config {
	cfg := defaultCfg.(*otlpreceiver.Config)
	cfg.HTTP.Endpoint = addr
	cfg.GRPC = nil
	return cfg
}

func startAndCleanup(t *testing.T, cmp component.Component) {
	require.NoError(t, cmp.Start(context.Background(), componenttest.NewNopHost()))
	t.Cleanup(func() {
		require.NoError(t, cmp.Shutdown(context.Background()))
	})
}

func TestErrorResponses(t *testing.T) {
	addr := testutil.GetAvailableLocalAddress(t)
	errMsgPrefix := fmt.Sprintf("error exporting items, request to http://%s/v1/traces responded with HTTP Status Code ", addr)

	tests := []struct {
		name           string
		responseStatus int
		responseBody   *status.Status
		err            error
		isPermErr      bool
		headers        map[string]string
	}{
		{
			name:           "400",
			responseStatus: http.StatusBadRequest,
			responseBody:   status.New(codes.InvalidArgument, "Bad field"),
			isPermErr:      true,
		},
		{
			name:           "404",
			responseStatus: http.StatusNotFound,
			err:            errors.New(errMsgPrefix + "404"),
		},
		{
			name:           "419",
			responseStatus: http.StatusTooManyRequests,
			responseBody:   status.New(codes.InvalidArgument, "Quota exceeded"),
			err: exporterhelper.NewThrottleRetry(
				errors.New(errMsgPrefix+"429, Message=Quota exceeded, Details=[]"),
				time.Duration(0)*time.Second),
		},
		{
			name:           "503",
			responseStatus: http.StatusServiceUnavailable,
			responseBody:   status.New(codes.InvalidArgument, "Server overloaded"),
			err: exporterhelper.NewThrottleRetry(
				errors.New(errMsgPrefix+"503, Message=Server overloaded, Details=[]"),
				time.Duration(0)*time.Second),
		},
		{
			name:           "503-Retry-After",
			responseStatus: http.StatusServiceUnavailable,
			responseBody:   status.New(codes.InvalidArgument, "Server overloaded"),
			headers:        map[string]string{"Retry-After": "30"},
			err: exporterhelper.NewThrottleRetry(
				errors.New(errMsgPrefix+"503, Message=Server overloaded, Details=[]"),
				time.Duration(30)*time.Second),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/v1/traces", func(writer http.ResponseWriter, request *http.Request) {
				for k, v := range test.headers {
					writer.Header().Add(k, v)
				}
				writer.WriteHeader(test.responseStatus)
				if test.responseBody != nil {
					msg, err := proto.Marshal(test.responseBody.Proto())
					require.NoError(t, err)
					_, err = writer.Write(msg)
					require.NoError(t, err)
				}
			})
			srv := http.Server{
				Addr:    addr,
				Handler: mux,
			}
			ln, err := net.Listen("tcp", addr)
			require.NoError(t, err)
			go func() {
				_ = srv.Serve(ln)
			}()

			cfg := &Config{
				ExporterSettings: config.NewExporterSettings(config.NewComponentID(typeStr)),
				TracesEndpoint:   fmt.Sprintf("http://%s/v1/traces", addr),
				// Create without QueueSettings and RetrySettings so that ConsumeTraces
				// returns the errors that we want to check immediately.
			}
			exp, err := createTracesExporter(context.Background(), componenttest.NewNopExporterCreateSettings(), cfg)
			require.NoError(t, err)

			// start the exporter
			err = exp.Start(context.Background(), componenttest.NewNopHost())
			require.NoError(t, err)
			t.Cleanup(func() {
				require.NoError(t, exp.Shutdown(context.Background()))
			})

			// generate traces
			traces := ptrace.NewTraces()
			err = exp.ConsumeTraces(context.Background(), traces)
			assert.Error(t, err)

			if test.isPermErr {
				assert.True(t, consumererror.IsPermanent(err))
			} else {
				assert.EqualValues(t, test.err, err)
			}

			srv.Close()
		})
	}
}

func TestUserAgent(t *testing.T) {
	addr := testutil.GetAvailableLocalAddress(t)
	set := componenttest.NewNopExporterCreateSettings()
	set.BuildInfo.Description = "Collector"
	set.BuildInfo.Version = "1.2.3test"

	tests := []struct {
		name       string
		headers    map[string]string
		expectedUA string
	}{
		{
			name:       "default_user_agent",
			expectedUA: "Collector/1.2.3test",
		},
		{
			name:       "custom_user_agent",
			headers:    map[string]string{"User-Agent": "My Custom Agent"},
			expectedUA: "My Custom Agent",
		},
		{
			name:       "custom_user_agent_lowercase",
			headers:    map[string]string{"user-agent": "My Custom Agent"},
			expectedUA: "My Custom Agent",
		},
	}

	t.Run("traces", func(t *testing.T) {
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				mux := http.NewServeMux()
				mux.HandleFunc("/v1/traces", func(writer http.ResponseWriter, request *http.Request) {
					assert.Contains(t, request.Header.Get("user-agent"), test.expectedUA)
					writer.WriteHeader(200)
				})
				srv := http.Server{
					Addr:    addr,
					Handler: mux,
				}
				ln, err := net.Listen("tcp", addr)
				require.NoError(t, err)
				go func() {
					_ = srv.Serve(ln)
				}()

				cfg := &Config{
					ExporterSettings: config.NewExporterSettings(config.NewComponentID(typeStr)),
					TracesEndpoint:   fmt.Sprintf("http://%s/v1/traces", addr),
					HTTPClientSettings: confighttp.HTTPClientSettings{
						Headers: test.headers,
					},
				}
				exp, err := createTracesExporter(context.Background(), set, cfg)
				require.NoError(t, err)

				// start the exporter
				err = exp.Start(context.Background(), componenttest.NewNopHost())
				require.NoError(t, err)
				t.Cleanup(func() {
					require.NoError(t, exp.Shutdown(context.Background()))
				})

				// generate data
				traces := ptrace.NewTraces()
				err = exp.ConsumeTraces(context.Background(), traces)
				require.NoError(t, err)

				srv.Close()
			})
		}
	})

	t.Run("metrics", func(t *testing.T) {
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				mux := http.NewServeMux()
				mux.HandleFunc("/v1/metrics", func(writer http.ResponseWriter, request *http.Request) {
					assert.Contains(t, request.Header.Get("user-agent"), test.expectedUA)
					writer.WriteHeader(200)
				})
				srv := http.Server{
					Addr:    addr,
					Handler: mux,
				}
				ln, err := net.Listen("tcp", addr)
				require.NoError(t, err)
				go func() {
					_ = srv.Serve(ln)
				}()

				cfg := &Config{
					ExporterSettings: config.NewExporterSettings(config.NewComponentID(typeStr)),
					MetricsEndpoint:  fmt.Sprintf("http://%s/v1/metrics", addr),
					HTTPClientSettings: confighttp.HTTPClientSettings{
						Headers: test.headers,
					},
				}
				exp, err := createMetricsExporter(context.Background(), set, cfg)
				require.NoError(t, err)

				// start the exporter
				err = exp.Start(context.Background(), componenttest.NewNopHost())
				require.NoError(t, err)
				t.Cleanup(func() {
					require.NoError(t, exp.Shutdown(context.Background()))
				})

				// generate data
				metrics := pmetric.NewMetrics()
				err = exp.ConsumeMetrics(context.Background(), metrics)
				require.NoError(t, err)

				srv.Close()
			})
		}
	})

	t.Run("logs", func(t *testing.T) {
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				mux := http.NewServeMux()
				mux.HandleFunc("/v1/logs", func(writer http.ResponseWriter, request *http.Request) {
					assert.Contains(t, request.Header.Get("user-agent"), test.expectedUA)
					writer.WriteHeader(200)
				})
				srv := http.Server{
					Addr:    addr,
					Handler: mux,
				}
				ln, err := net.Listen("tcp", addr)
				require.NoError(t, err)
				go func() {
					_ = srv.Serve(ln)
				}()

				cfg := &Config{
					ExporterSettings: config.NewExporterSettings(config.NewComponentID(typeStr)),
					LogsEndpoint:     fmt.Sprintf("http://%s/v1/logs", addr),
					HTTPClientSettings: confighttp.HTTPClientSettings{
						Headers: test.headers,
					},
				}
				exp, err := createLogsExporter(context.Background(), set, cfg)
				require.NoError(t, err)

				// start the exporter
				err = exp.Start(context.Background(), componenttest.NewNopHost())
				require.NoError(t, err)
				t.Cleanup(func() {
					require.NoError(t, exp.Shutdown(context.Background()))
				})

				// generate data
				logs := plog.NewLogs()
				err = exp.ConsumeLogs(context.Background(), logs)
				require.NoError(t, err)

				srv.Close()

			})
		}
	})
}
