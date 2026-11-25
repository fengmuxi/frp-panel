//go:build !windows
// +build !windows

package wg

import (
	"context"
	"errors"
	"fmt"

	"github.com/VaalaCat/frp-panel/defs"
	"github.com/VaalaCat/frp-panel/services/app"
	"github.com/VaalaCat/frp-panel/utils"
)

var (
	_ app.WireGuardManager = (*wireGuardManager)(nil)
)

type wireGuardManager struct {
	services *utils.SyncMap[string, app.WireGuard]

	originCtx *app.Context
	svcCtx    *app.Context
	cancel    context.CancelFunc
}

func NewWireGuardManager(appInstance app.Application) app.WireGuardManager {
	ctx := app.NewContext(context.Background(), appInstance)

	svcCtx, cancel := ctx.CopyWithCancel()
	return &wireGuardManager{
		services:  &utils.SyncMap[string, app.WireGuard]{},
		originCtx: ctx,
		svcCtx:    svcCtx,
		cancel:    cancel,
	}
}

func (m *wireGuardManager) CreateService(cfg *defs.WireGuardConfig) (app.WireGuard, error) {
	if cfg == nil {
		return nil, errors.New("wireguard config is nil")
	}

	if cfg.GetInterfaceName() == "" {
		return nil, errors.New("wireguard config interface name is empty")
	}

	ctx := m.svcCtx

	wg, err := NewWireGuard(ctx, *cfg, ctx.Logger().WithField("interface", cfg.GetInterfaceName()))
	if err != nil {
		return nil, errors.Join(errors.New("wireguard manager create wireguard error"), err)
	}

	// 覆盖前先停旧实例：事件重试/与 pull 并发时重发 CREATE 会覆盖 map 条目，
	// 旧实例的 TUN 设备与 goroutine 全部泄漏
	if old, ok := m.services.Load(cfg.GetInterfaceName()); ok {
		_ = old.Stop()
	}
	m.services.Store(cfg.GetInterfaceName(), wg)
	return wg, nil
}

func (m *wireGuardManager) StartService(interfaceName string) error {
	wg, ok := m.services.Load(interfaceName)
	if !ok {
		return fmt.Errorf("wireguard service not found, interfaceName: %s", interfaceName)
	}

	if err := wg.Start(); err != nil {
		return errors.Join(errors.New("wireguard manager start wireguard error"), err)
	}
	return nil
}

func (m *wireGuardManager) GetService(interfaceName string) (app.WireGuard, bool) {
	return m.services.Load(interfaceName)
}

func (m *wireGuardManager) GetAllServices() []app.WireGuard {
	return m.services.Values()
}

func (m *wireGuardManager) StopService(interfaceName string) error {
	wg, ok := m.services.Load(interfaceName)
	if !ok {
		return fmt.Errorf("wireguard service not found, interfaceName: %s", interfaceName)
	}

	if err := wg.Stop(); err != nil {
		return errors.Join(errors.New("wireguard manager stop wireguard error"), err)
	}
	return nil
}

func (m *wireGuardManager) RemoveService(interfaceName string) error {
	wg, ok := m.services.Load(interfaceName)
	if !ok {
		return fmt.Errorf("wireguard service not found, interfaceName: %s", interfaceName)
	}

	if err := wg.Stop(); err != nil {
		return errors.Join(errors.New("wireguard manager remove wireguard error"), err)
	}

	m.services.Delete(interfaceName)
	return nil
}

func (m *wireGuardManager) StopAllServices() map[string]error {
	errMap := make(map[string]error)
	// 首个失败不中断遍历：尽力停止全部服务并收集错误
	m.services.Range(func(k string, v app.WireGuard) bool {
		if err := v.Stop(); err != nil {
			m.svcCtx.Logger().WithError(err).Errorf("wireguard manager stop all wireguard error, interfaceName: %s", k)
			errMap[k] = err
		}
		m.services.Delete(k)
		return true
	})
	m.cancel()
	m.svcCtx, m.cancel = m.originCtx.BackgroundWithCancel()
	return errMap
}

func (m *wireGuardManager) RestartService(interfaceName string) error {
	wg, ok := m.services.Load(interfaceName)
	if !ok {
		return fmt.Errorf("wireguard service not found, interfaceName: %s", interfaceName)
	}

	if err := wg.Stop(); err != nil {
		return errors.Join(errors.New("wireguard manager restart wireguard error"), err)
	}

	if err := wg.Start(); err != nil {
		return errors.Join(errors.New("wireguard manager restart wireguard error"), err)
	}

	return nil
}

func (m *wireGuardManager) Start() {
	<-m.svcCtx.Done()
}

func (m *wireGuardManager) Stop() {
	m.StopAllServices()
}
