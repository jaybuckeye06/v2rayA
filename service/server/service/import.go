package service

import (
	"fmt"
	url2 "net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/v2rayA/v2rayA/conf"
	"github.com/v2rayA/v2rayA/pkg/util/log"

	"github.com/v2rayA/v2rayA/common"
	"github.com/v2rayA/v2rayA/common/httpClient"
	"github.com/v2rayA/v2rayA/common/resolv"
	"github.com/v2rayA/v2rayA/core/serverObj"
	"github.com/v2rayA/v2rayA/core/touch"
	"github.com/v2rayA/v2rayA/core/v2ray"
	"github.com/v2rayA/v2rayA/db/configure"
)

func PluginManagerValidateLink(url string) bool {
	if pm := conf.GetEnvironmentConfig().PluginManager; pm != "" {
		_, err := serverObj.NewFromLink(serverObj.PluginManagerScheme, url)
		return err == nil
	} else {
		return false
	}
}

func Import(url string, which *configure.Which, filters []string) (err error) {
	//log.Trace(url)
	resolv.CheckResolvConf()
	url = strings.TrimSpace(url)
	if lines := strings.Split(url, "\n"); len(lines) >= 2 || strings.HasPrefix(url, "{") {
		infos, _, err := ResolveByLines(url)
		if err != nil {
			return fmt.Errorf("failed to resolve addresses: %w", err)
		}
		// Apply filters if any
		if len(filters) > 0 {
			filteredInfos := make([]serverObj.ServerObj, 0)
			for _, info := range infos {
				if matchesFilters(info, filters) {
					filteredInfos = append(filteredInfos, info)
				}
			}
			infos = filteredInfos
		}
		for _, info := range infos {
			err = configure.AppendServers([]*configure.ServerRaw{{ServerObj: info}})
		}
		if err != nil {
			return err
		}
		// Auto connect top 8 servers
		if err = autoConnectTopServers(8); err != nil {
			log.Warn("Auto connect failed: %v", err)
		}
		return nil
	}
	supportedPrefix := []string{"vmess", "vless", "ss", "ssr", "trojan", "trojan-go", "http-proxy",
		"https-proxy", "socks5", "http2", "juicity", "tuic"}
	for i := range supportedPrefix {
		supportedPrefix[i] += "://"
	}
	if PluginManagerValidateLink(url) || common.HasAnyPrefix(url, supportedPrefix) {
		var obj serverObj.ServerObj
		obj, err = ResolveURL(url)
		if err != nil {
			return
		}
		// Apply filters for single server
		if len(filters) > 0 && !matchesFilters(obj, filters) {
			return fmt.Errorf("server does not match any of the filters")
		}
		if which != nil {
			// the request is to modify a server
			ind := which.ID - 1
			if which.TYPE != configure.ServerType || ind < 0 || ind >= configure.GetLenServers() {
				return fmt.Errorf("bad request")
			}
			var sr *configure.ServerRaw
			sr, err = which.LocateServerRaw()
			if err != nil {
				return
			}
			sr.ServerObj = obj
			if err = configure.SetServer(ind, &configure.ServerRaw{ServerObj: obj}); err != nil {
				return
			}
			css := configure.GetConnectedServers()
			if css.Len() > 0 {
				for _, cs := range css.Get() {
					if which.TYPE == cs.TYPE && which.ID == cs.ID {
						if err = v2ray.UpdateV2RayConfig(); err != nil {
							return
						}
					}
				}
			}
		} else {
			// append a server
			err = configure.AppendServers([]*configure.ServerRaw{{ServerObj: obj}})
			if err != nil {
				return err
			}
			// Auto connect the new server
			if err = autoConnectTopServers(1); err != nil {
				log.Warn("Auto connect failed: %v", err)
			}
		}
	} else {
		// subscription
		source := url
		if u, err := url2.Parse(source); err == nil {
			if u.Scheme == "sub" {
				var e error
				source, e = common.Base64StdDecode(source[6:])
				if e != nil {
					source, _ = common.Base64URLDecode(source[6:])
				}
			} else if u.Scheme == "" {
				u.Scheme = "http"
				source = u.String()
			}
		}
		c := httpClient.GetHttpClientAutomatically()
		c.Timeout = 90 * time.Second
		infos, status, err := ResolveSubscriptionWithClient(source, c)
		if err != nil {
			return fmt.Errorf("failed to resolve subscription address: %w", err)
		}

		// Apply filters if any
		if len(filters) > 0 {
			filteredInfos := make([]serverObj.ServerObj, 0)
			for _, info := range infos {
				if matchesFilters(info, filters) {
					filteredInfos = append(filteredInfos, info)
				}
			}
			infos = filteredInfos
		}

		// info to serverRawV2
		servers := make([]configure.ServerRaw, len(infos))
		for i, v := range infos {
			servers[i] = configure.ServerRaw{ServerObj: v}
		}

		// deduplicate
		unique := make(map[configure.ServerRaw]interface{})
		for _, s := range servers {
			unique[s] = nil
		}
		uniqueServers := make([]configure.ServerRaw, 0)
		for _, s := range servers {
			if _, ok := unique[s]; ok {
				uniqueServers = append(uniqueServers, s)
				delete(unique, s)
			}
		}
		err = configure.AppendSubscriptions([]*configure.SubscriptionRaw{{
			Address: source,
			Status:  string(touch.NewUpdateStatus()),
			Servers: uniqueServers,
			Info:    status,
			Filters: filters,
		}})
		if err != nil {
			return err
		}
		// Auto connect top 8 servers from subscription
		if err = autoConnectTopServers(8); err != nil {
			log.Warn("Auto connect failed: %v", err)
		}
	}
	return
}

// autoConnectTopServers connects the top N servers based on latency
func autoConnectTopServers(n int) error {
	// Get all servers from main list and subscriptions
	servers := configure.GetServers()
	subscriptions := configure.GetSubscriptions()

	// Create a list of all available servers
	type serverWithLatency struct {
		server  configure.ServerRaw
		latency int64
		source  string // "main" or "subscription"
		subID   int    // subscription ID if source is "subscription"
		index   int    // original 1-based index in the server list
	}
	serverList := make([]serverWithLatency, 0)

	// Add main servers
	for i, server := range servers {
		serverList = append(serverList, serverWithLatency{
			server:  server,
			latency: 99999, // Default high latency
			source:  "main",
			index:   i + 1, // Store original 1-based index
		})
	}

	// Add subscription servers
	for subIndex, subscription := range subscriptions {
		for i, server := range subscription.Servers {
			serverList = append(serverList, serverWithLatency{
				server:  server,
				latency: 99999, // Default high latency
				source:  "subscription",
				subID:   subIndex + 1,
				index:   i + 1, // Store original 1-based index
			})
		}
	}

	log.Info("Available servers count: %d", len(serverList))
	for i, server := range serverList {
		log.Info("Server %d: %s (Source: %s)", i+1, server.server.ServerObj.GetName(), server.source)
	}

	if len(serverList) == 0 {
		return fmt.Errorf("no servers available")
	}

	// Ping all servers to get latency
	for i := range serverList {
		server := &serverList[i]
		which := &configure.Which{
			TYPE: configure.ServerType,
			ID:   server.index, // Use original 1-based index
		}
		if server.source == "subscription" {
			which.TYPE = configure.SubscriptionServerType
			which.Sub = server.subID - 1 // Convert to 0-based index
		}

		err := which.Ping(5 * time.Second) // 5 second timeout
		if err != nil {
			log.Warn("Failed to ping server %s: %v", server.server.ServerObj.GetName(), err)
			continue
		}

		// Parse latency from string like "123ms"
		if latency, err := strconv.ParseInt(strings.TrimSuffix(which.Latency, "ms"), 10, 64); err == nil {
			server.latency = latency
			server.server.Latency = which.Latency
			log.Info("Server %s ping result: %s", server.server.ServerObj.GetName(), which.Latency)
		}
	}

	// Sort by latency
	sort.Slice(serverList, func(i, j int) bool {
		return serverList[i].latency < serverList[j].latency
	})

	// Take top N servers
	n = min(n, len(serverList))
	log.Info("Selected top %d servers for connection:", n)
	for i := 0; i < n; i++ {
		server := serverList[i]
		log.Info("  %d. %s (Source: %s, Latency: %s)",
			i+1,
			server.server.ServerObj.GetName(),
			server.source,
			server.server.Latency)
	}

	// Connect to selected servers
	for i := 0; i < n; i++ {
		server := serverList[i]
		which := &configure.Which{
			TYPE: configure.ServerType,
			ID:   server.index, // Use original 1-based index
		}
		if server.source == "subscription" {
			which.TYPE = configure.SubscriptionServerType
			which.Sub = server.subID - 1 // Convert to 0-based index
		}
		log.Info("Connecting to server %s...", server.server.ServerObj.GetName())
		if err := Connect(which); err != nil {
			log.Warn("Failed to connect server %s: %v", server.server.ServerObj.GetName(), err)
			continue
		}
		log.Info("Successfully connected to server %s", server.server.ServerObj.GetName())
	}

	return nil
}

// matchesFilters checks if a server matches any of the provided filters
func matchesFilters(server serverObj.ServerObj, filters []string) bool {
	if len(filters) == 0 {
		return true
	}
	serverName := strings.ToLower(server.GetName())
	for _, filter := range filters {
		filter = strings.TrimSpace(filter)
		if filter == "" {
			continue
		}
		if !strings.Contains(serverName, strings.ToLower(filter)) {
			return false
		}
	}
	return true
}
