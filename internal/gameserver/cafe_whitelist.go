package gameserver

import (
	_ "embed"
	"encoding/json"
	"sync"

	"rf4server/internal/store"
)

// 咖啡厅悬赏鱼白名单已固化到静态文件，随程序打包，后台不再维护。
// 决定鱼种是否算稀有/巨物（影响进长期位、巨物补贴、普通位排除）。
//
//go:embed assets/cafe_bounty_whitelist.json
var cafeWhitelistJSON []byte

var cafeWhitelistCache struct {
	sync.Once
	byKey map[string]store.CafeFish
}

// cafeWhitelistOverride 仅供测试注入白名单，生产恒为 nil。
var cafeWhitelistOverride map[string]store.CafeFish

// cafeWhitelistMap 返回 fish_key -> 白名单行；解析失败则返回空表，保证服务可用。
func cafeWhitelistMap() map[string]store.CafeFish {
	if cafeWhitelistOverride != nil {
		return cafeWhitelistOverride
	}
	cafeWhitelistCache.Do(func() {
		cafeWhitelistCache.byKey = map[string]store.CafeFish{}
		var data struct {
			Fish []store.CafeFish `json:"fish"`
		}
		if err := json.Unmarshal(cafeWhitelistJSON, &data); err != nil {
			return
		}
		for _, f := range data.Fish {
			cafeWhitelistCache.byKey[f.FishKey] = f
		}
	})
	return cafeWhitelistCache.byKey
}
