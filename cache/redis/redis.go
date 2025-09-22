package redis

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/go-spatial/tegola"
	"github.com/go-spatial/tegola/cache"
	"github.com/go-spatial/tegola/dict"
)

const CacheType = "redis"

const (
	ConfigKeyNetwork  = "network"
	ConfigKeyAddress  = "address"
	ConfigKeyPassword = "password"
	ConfigKeyDB       = "db"
	ConfigKeyMaxZoom  = "max_zoom"
	ConfigKeyTTL      = "ttl"
	ConfigKeySSL      = "ssl"
	ConfigKeyURI      = "uri"
)

var (
	// default values
	defaultNetwork  = "tcp"
	defaultAddress  = "127.0.0.1:6379"
	defaultPassword = ""
	defaultURI      = ""
	defaultDB       = 0
	defaultMaxZoom  = uint(tegola.MaxZ)
	defaultTTL      = 0
	defaultSSL      = false
)

func init() {
	cache.Register(CacheType, New)
}

// TODO @iwpnd: deprecate connection with Addr
// CreateOptions creates redis.Options from an implicit or explicit c
func CreateOptions(c dict.Dicter) (opts *redis.Options, err error) {
	uri, err := c.String(ConfigKeyURI, &defaultURI)
	if err != nil {
		return nil, err
	}

	if uri != "" {
		opts, err := redis.ParseURL(uri)
		if err != nil {
			return nil, err
		}

		return opts, nil
	}

	network, err := c.String(ConfigKeyNetwork, &defaultNetwork)
	if err != nil {
		return nil, err
	}

	addr, err := c.String(ConfigKeyAddress, &defaultAddress)
	if err != nil {
		return nil, err
	}

	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}

	if host == "" {
		return nil, &ErrHostMissing{msg: fmt.Sprintf("no host provided in '%s'", addr)}
	}

	password, err := c.String(ConfigKeyPassword, &defaultPassword)
	if err != nil {
		return nil, err
	}

	db, err := c.Int(ConfigKeyDB, &defaultDB)
	if err != nil {
		return nil, err
	}

	o := &redis.Options{
		Network:     network,
		Addr:        addr,
		Password:    password,
		DB:          db,
		PoolSize:    2,
		DialTimeout: 3 * time.Second,
	}

	ssl, err := c.Bool(ConfigKeySSL, &defaultSSL)
	if err != nil {
		return nil, err
	}

	if ssl {
		o.TLSConfig = &tls.Config{ServerName: host}
	}

	return o, nil
}

func New(c dict.Dicter) (rcache cache.Interface, err error) {
	ctx := context.Background()
	opts, err := CreateOptions(c)
	if err != nil {
		return nil, err
	}
	client := redis.NewClient(opts)

	pong, err := client.Ping(ctx).Result()
	if err != nil {
		fmt.Println("Error connecting to Redis:", err)
		return nil, err
	}
	fmt.Println("Redis connected successfully:", pong)
	if pong != "PONG" {
		return nil, fmt.Errorf("redis did not respond with 'PONG', '%s'", pong)
	}

	// the c map's underlying value is int
	maxZoom, err := c.Uint(ConfigKeyMaxZoom, &defaultMaxZoom)
	if err != nil {
		return nil, err
	}

	ttl, err := c.Int(ConfigKeyTTL, &defaultTTL)
	if err != nil {
		return nil, err
	}

	return &RedisCache{
		Redis:      client,
		MaxZoom:    maxZoom,
		Expiration: time.Duration(ttl) * time.Second,
	}, nil
}

type RedisCache struct {
	Redis      *redis.Client
	Expiration time.Duration
	MaxZoom    uint
}

func (rdc *RedisCache) Set(ctx context.Context, key *cache.Key, val []byte) error {
	fmt.Printf("RedisCache.Set called for key: %s, data size: %d bytes, MaxZoom: %d\n", key.String(), len(val), rdc.MaxZoom)

	if key.Z > rdc.MaxZoom {
		fmt.Printf("RedisCache.Set: skipping cache due to zoom level %d > MaxZoom %d\n", key.Z, rdc.MaxZoom)
		return nil
	}

	keyStr := key.String()
	fmt.Printf("RedisCache.Set: About to call Redis.Set with key: '%s', expiration: %v\n", keyStr, rdc.Expiration)
	fmt.Printf("RedisCache.Set: Redis client database: %d\n", rdc.Redis.Options().DB)

	err := rdc.Redis.Set(ctx, keyStr, val, rdc.Expiration).Err()
	if err != nil {
		fmt.Printf("RedisCache.Set error: %v\n", err)
	} else {
		fmt.Printf("RedisCache.Set success for key: %s\n", keyStr)

		// 验证数据是否真的写入了
		testVal, testErr := rdc.Redis.Get(ctx, keyStr).Bytes()
		if testErr != nil {
			fmt.Printf("RedisCache.Set verification failed - cannot read back key: %v\n", testErr)
		} else {
			fmt.Printf("RedisCache.Set verification success - read back %d bytes\n", len(testVal))
		}

		// 检查键是否真的存在
		exists := rdc.Redis.Exists(ctx, keyStr).Val()
		fmt.Printf("RedisCache.Set: Key exists check: %d\n", exists)

		// 获取键的 TTL
		ttl := rdc.Redis.TTL(ctx, keyStr).Val()
		fmt.Printf("RedisCache.Set: Key TTL: %v\n", ttl)
	}
	return err
}

func (rdc *RedisCache) Get(ctx context.Context, key *cache.Key) (val []byte, hit bool, err error) {
	val, err = rdc.Redis.Get(ctx, key.String()).Bytes()

	switch err {
	case nil: // cache hit
		return val, true, nil
	case redis.Nil: // cache miss
		return val, false, nil
	default: // error
		return val, false, err
	}
}

func (rdc *RedisCache) Purge(ctx context.Context, key *cache.Key) (err error) {
	return rdc.Redis.Del(ctx, key.String()).Err()
}
