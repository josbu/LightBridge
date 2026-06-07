package modules

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/encoding"
)

const providerAdapterService = "lightbridge.modules.ProviderAdapter"
const jsonCodecName = "json"

type jsonCodec struct{}

func (jsonCodec) Name() string { return jsonCodecName }
func (jsonCodec) Marshal(v any) ([]byte, error) {
	if v == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(v)
}
func (jsonCodec) Unmarshal(data []byte, v any) error {
	if len(data) == 0 || v == nil {
		return nil
	}
	return json.Unmarshal(data, v)
}
func init() { encoding.RegisterCodec(jsonCodec{}) }

type grpcProviderAdapter struct{ conn *grpc.ClientConn }

func NewGRPCProviderAdapter(socketPath string) (ProviderAdapter, error) {
	dialer := func(ctx context.Context, addr string) (net.Conn, error) { return net.Dial("unix", socketPath) }
	conn, err := grpc.NewClient("unix://"+socketPath, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(dialer), grpc.WithDefaultCallOptions(grpc.ForceCodec(jsonCodec{})))
	if err != nil {
		return nil, err
	}
	return &grpcProviderAdapter{conn: conn}, nil
}
func (a *grpcProviderAdapter) Close() error {
	if a == nil || a.conn == nil {
		return nil
	}
	return a.conn.Close()
}
func method(name string) string { return "/" + providerAdapterService + "/" + name }
func (a *grpcProviderAdapter) TestAccount(ctx context.Context, req TestAccountRequest) (*TestAccountResult, error) {
	var out TestAccountResult
	if err := a.conn.Invoke(ctx, method("TestAccount"), &req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
func (a *grpcProviderAdapter) Forward(ctx context.Context, req GatewayRequest) (<-chan GatewayEvent, error) {
	stream, err := a.conn.NewStream(ctx, &grpc.StreamDesc{ServerStreams: true}, method("Forward"))
	if err != nil {
		return nil, err
	}
	if err := stream.SendMsg(&req); err != nil {
		return nil, err
	}
	_ = stream.CloseSend()
	ch := make(chan GatewayEvent)
	go func() {
		defer close(ch)
		for {
			var ev GatewayEvent
			err := stream.RecvMsg(&ev)
			if err == io.EOF {
				return
			}
			if err != nil {
				ch <- GatewayEvent{Type: "error", Error: &GatewayError{Code: "grpc_error", Message: err.Error()}}
				return
			}
			if strings.TrimSpace(ev.Type) == "" {
				ev.Type = "data"
			}
			ch <- ev
		}
	}()
	return ch, nil
}
