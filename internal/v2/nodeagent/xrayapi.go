package nodeagent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/xtls/xray-core/app/proxyman/command"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/proxy/trojan"
	"github.com/xtls/xray-core/proxy/vless"
	"github.com/xtls/xray-core/proxy/vmess"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"
)

// UserAPI changes users of a running Xray without a restart.
type UserAPI interface {
	Supports(protocol string) bool
	AddUser(ctx context.Context, u ConfigUser) error
	RemoveUser(ctx context.Context, u ConfigUser) error
}

// XrayUsers uses the stock Xray HandlerService on loopback.
type XrayUsers struct {
	Addr string

	mu   sync.Mutex
	conn *grpc.ClientConn
}

func (x *XrayUsers) client() (command.HandlerServiceClient, error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.conn == nil {
		c, err := grpc.NewClient(x.Addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return nil, err
		}
		x.conn = c
	}
	return command.NewHandlerServiceClient(x.conn), nil
}

func (x *XrayUsers) Supports(p string) bool {
	return p == "vless" || p == "vmess" || p == "trojan"
}

func account(u ConfigUser) (proto.Message, error) {
	switch u.Protocol {
	case "vless":
		return &vless.Account{Id: u.ID, Flow: u.Flow, Encryption: "none"}, nil
	case "vmess":
		return &vmess.Account{Id: u.ID}, nil
	case "trojan":
		return &trojan.Account{Password: u.Password}, nil
	}
	return nil, fmt.Errorf("protocol %s has no live user API", u.Protocol)
}

func (x *XrayUsers) AddUser(ctx context.Context, u ConfigUser) error {
	acc, err := account(u)
	if err != nil {
		return err
	}
	c, err := x.client()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	op := &command.AddUserOperation{User: &protocol.User{Email: u.Email, Account: serial.ToTypedMessage(acc)}}
	_, err = c.AlterInbound(ctx, &command.AlterInboundRequest{Tag: u.Tag, Operation: serial.ToTypedMessage(op)})
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "already exists") {
		return nil
	}
	return err
}

func (x *XrayUsers) RemoveUser(ctx context.Context, u ConfigUser) error {
	c, err := x.client()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	op := &command.RemoveUserOperation{Email: u.Email}
	_, err = c.AlterInbound(ctx, &command.AlterInboundRequest{Tag: u.Tag, Operation: serial.ToTypedMessage(op)})
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "not found") {
		return nil
	}
	return err
}

func (x *XrayUsers) Close() error {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.conn == nil {
		return nil
	}
	err := x.conn.Close()
	x.conn = nil
	return err
}
