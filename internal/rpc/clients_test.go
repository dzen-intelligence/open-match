// Copyright 2019 Google LLC
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

package rpc

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"testing"

	"github.com/spf13/viper"
	"open-match.dev/open-match/internal/config"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"open-match.dev/open-match/internal/pb"
	shellTesting "open-match.dev/open-match/internal/testing"
	netlistenerTesting "open-match.dev/open-match/internal/util/netlistener/testing"
	certgenTesting "open-match.dev/open-match/tools/certgen/testing"
)

func TestSecureGRPCFromConfig(t *testing.T) {
	assert := assert.New(t)

	cfg, rpcParams, closer := configureConfigAndKeysForTesting(assert, true)
	defer closer()

	runGrpcClientTests(assert, cfg, rpcParams)
}

func TestInsecureGRPCFromConfig(t *testing.T) {
	assert := assert.New(t)

	cfg, rpcParams, closer := configureConfigAndKeysForTesting(assert, false)
	defer closer()

	runGrpcClientTests(assert, cfg, rpcParams)
}

func TestHTTPSFromConfig(t *testing.T) {
	assert := assert.New(t)

	cfg, rpcParams, closer := configureConfigAndKeysForTesting(assert, true)
	defer closer()

	runHTTPClientTests(assert, cfg, rpcParams)
}
func TestInsecureHTTPFromConfig(t *testing.T) {
	assert := assert.New(t)

	cfg, rpcParams, closer := configureConfigAndKeysForTesting(assert, false)
	defer closer()

	runHTTPClientTests(assert, cfg, rpcParams)
}

func runGrpcClientTests(assert *assert.Assertions, cfg config.View, rpcParams *ServerParams) {
	// Serve a fake frontend server and wait for its full start up
	ff := &shellTesting.FakeFrontend{}
	rpcParams.AddHandleFunc(func(s *grpc.Server) {
		pb.RegisterFrontendServer(s, ff)
	}, pb.RegisterFrontendHandlerFromEndpoint)

	s := &Server{}
	defer s.Stop()
	waitForStart, err := s.Start(rpcParams)
	assert.Nil(err)
	waitForStart()

	// Acquire grpc client
	grpcConn, err := GRPCClientFromConfig(cfg, "test")
	assert.Nil(err)
	assert.NotNil(grpcConn)

	// Confirm the client works as expected
	ctx := context.Background()
	feClient := pb.NewFrontendClient(grpcConn)
	grpcResp, err := feClient.CreateTicket(ctx, &pb.CreateTicketRequest{})
	assert.Nil(err)
	assert.NotNil(grpcResp)
}

func runHTTPClientTests(assert *assert.Assertions, cfg config.View, rpcParams *ServerParams) {
	// Serve a fake frontend server and wait for its full start up
	ff := &shellTesting.FakeFrontend{}
	rpcParams.AddHandleFunc(func(s *grpc.Server) {
		pb.RegisterFrontendServer(s, ff)
	}, pb.RegisterFrontendHandlerFromEndpoint)
	s := &Server{}
	defer s.Stop()
	waitForStart, err := s.Start(rpcParams)
	assert.Nil(err)
	waitForStart()

	// Acquire http client
	httpClient, baseURL, err := HTTPClientFromConfig(cfg, "test")
	assert.Nil(err)

	// Confirm the client works as expected
	httpReq, err := http.NewRequest(http.MethodGet, baseURL+"/healthz", nil)
	assert.Nil(err)
	assert.NotNil(httpReq)

	httpResp, err := httpClient.Do(httpReq)
	assert.Nil(err)
	assert.NotNil(httpResp)

	body, err := ioutil.ReadAll(httpResp.Body)
	assert.Nil(err)
	assert.Equal(200, httpResp.StatusCode)
	assert.Equal("ok", string(body))
}

// Generate a config view and optional TLS key manifests (optional) for testing
func configureConfigAndKeysForTesting(assert *assert.Assertions, tlsEnabled bool) (config.View, *ServerParams, func()) {
	// Create netlisteners on random ports used for rpc serving
	grpcLh := netlistenerTesting.MustListen()
	httpLh := netlistenerTesting.MustListen()
	rpcParams := NewServerParamsFromListeners(grpcLh, httpLh)

	// Generate a config view with paths to the manifests
	cfg := viper.New()
	cfg.Set("test.hostname", "localhost")
	cfg.Set("test.grpcport", grpcLh.Number())
	cfg.Set("test.httpport", httpLh.Number())

	// Create temporary TLS key files for testing
	pubFile, err := ioutil.TempFile("", "pub*")
	assert.Nil(err)

	if tlsEnabled {
		// Generate public and private key bytes
		pubBytes, priBytes, err := certgenTesting.CreateCertificateAndPrivateKeyForTesting([]string{
			fmt.Sprintf("localhost:%d", grpcLh.Number()),
			fmt.Sprintf("localhost:%d", httpLh.Number()),
		})
		assert.Nil(err)

		// Write certgen key bytes to the temp files
		err = ioutil.WriteFile(pubFile.Name(), pubBytes, 0400)
		assert.Nil(err)

		// Generate a config view with paths to the manifests
		cfg.Set("tls.enabled", true)
		cfg.Set("tls.trustedCertificatePath", pubFile.Name())

		rpcParams.SetTLSConfiguration(pubBytes, pubBytes, priBytes)
	}

	return cfg, rpcParams, func() { removeTempFile(assert, pubFile.Name()) }
}

func removeTempFile(assert *assert.Assertions, paths ...string) {
	for _, path := range paths {
		err := os.Remove(path)
		assert.Nil(err)
	}
}
