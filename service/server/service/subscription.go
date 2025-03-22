package service

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	jsoniter "github.com/json-iterator/go"
	"github.com/v2rayA/v2rayA/common"
	"github.com/v2rayA/v2rayA/common/httpClient"
	"github.com/v2rayA/v2rayA/common/resolv"
	"github.com/v2rayA/v2rayA/core/serverObj"
	"github.com/v2rayA/v2rayA/core/touch"
	"github.com/v2rayA/v2rayA/db/configure"
	"github.com/v2rayA/v2rayA/pkg/util/log"
)

type SIP008 struct {
	Version        int    `json:"version"`
	Username       string `json:"username"`
	UserUUID       string `json:"user_uuid"`
	BytesUsed      uint64 `json:"bytes_used"`
	BytesRemaining uint64 `json:"bytes_remaining"`
	Servers        []struct {
		Server     string `json:"server"`
		ServerPort int    `json:"server_port"`
		Password   string `json:"password"`
		Method     string `json:"method"`
		Plugin     string `json:"plugin"`
		PluginOpts string `json:"plugin_opts"`
		Remarks    string `json:"remarks"`
		ID         string `json:"id"`
	} `json:"servers"`
}

func resolveSIP008(raw string) (infos []serverObj.ServerObj, sip SIP008, err error) {
	err = jsoniter.Unmarshal([]byte(raw), &sip)
	if err != nil {
		return
	}
	for _, server := range sip.Servers {
		u := url.URL{
			Scheme:   "ss",
			User:     url.UserPassword(server.Method, server.Password),
			Host:     net.JoinHostPort(server.Server, strconv.Itoa(server.ServerPort)),
			RawQuery: url.Values{"plugin": []string{server.PluginOpts}}.Encode(),
			Fragment: server.Remarks,
		}
		obj, err := serverObj.NewFromLink("shadowsocks", u.String())
		if err != nil {
			return nil, SIP008{}, err
		}
		infos = append(infos, obj)
	}
	return
}

func resolveByLines(raw string) (infos []serverObj.ServerObj, status string, err error) {
	// 切分raw
	rows := strings.Split(strings.TrimSpace(raw), "\n")
	// 解析
	infos = make([]serverObj.ServerObj, 0)
	for _, row := range rows {
		if strings.HasPrefix(row, "STATUS=") {
			status = strings.TrimPrefix(row, "STATUS=")
			continue
		}
		var data serverObj.ServerObj
		data, err = ResolveURL(row)
		if err != nil {
			if !errors.Is(err, EmptyAddressErr) {
				log.Warn("resolveByLines: %v: %v", err, row)
			}
			err = nil
			continue
		}
		infos = append(infos, data)
	}
	return
}

type SubscriptionUserInfo struct {
	Upload   int64
	Download int64
	Total    int64
	Expire   time.Time
}

func (sui *SubscriptionUserInfo) String() string {
	var outputs []string
	if sui.Download != -1 {
		outputs = append(outputs, fmt.Sprintf("download: %v GB", sui.Download/1e9))
	}
	if sui.Upload != -1 {
		outputs = append(outputs, fmt.Sprintf("upload: %v GB", sui.Upload/1e9))
	}
	if sui.Total != -1 {
		outputs = append(outputs, fmt.Sprintf("total: %v GB", sui.Total/1e9))
	}
	if !sui.Expire.IsZero() {
		outputs = append(outputs, fmt.Sprintf("expire: %v UTC", sui.Expire.Format("2006-01-02 15:04")))
	}
	return strings.Join(outputs, "; ")
}

func parseSubscriptionUserInfo(str string) SubscriptionUserInfo {
	fields := strings.Split(str, ";")
	sui := SubscriptionUserInfo{
		Upload:   -1,
		Download: -1,
		Total:    -1,
		Expire:   time.Time{},
	}
	for _, field := range fields {
		field = strings.TrimSpace(field)
		kv := strings.SplitN(field, "=", 2)
		if len(kv) < 2 {
			continue
		}
		v, e := strconv.ParseInt(kv[1], 10, 64)
		if e != nil {
			continue
		}
		switch kv[0] {
		case "upload":
			sui.Upload = v
		case "download":
			sui.Download = v
		case "total":
			sui.Total = v
		case "expire":
			sui.Expire = time.Unix(v, 0).UTC()
		}
	}
	return sui
}
func trapBOM(fileBytes []byte) []byte {
	trimmedBytes := bytes.Trim(fileBytes, "\xef\xbb\xbf")
	return trimmedBytes
}
func ResolveSubscriptionWithClient(source string, client *http.Client) (infos []serverObj.ServerObj, status string, err error) {
	c := *client
	if c.Timeout < 30*time.Second {
		c.Timeout = 30 * time.Second
	}

	res, err := httpClient.HttpGetUsingSpecificClient(client, source)
	if err != nil {
		return
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, "", err
	}
	// base64 decode. trapBOM due to https://github.com/v2rayA/v2rayA/issues/612
	raw, err := common.Base64StdDecode(string(trapBOM(b)))
	if err != nil {
		raw, _ = common.Base64URLDecode(string(b))
	}
	infos, status, err = ResolveByLines(raw)
	if err != nil {
		return nil, "", err
	}
	subscriptionUserInfo := res.Header.Get("Subscription-Userinfo")
	sui := parseSubscriptionUserInfo(subscriptionUserInfo)
	if len(status) > 0 {
		status = sui.String() + "|" + status
	} else {
		status = sui.String()
	}
	return infos, status, nil
}

func ResolveByLines(raw string) (infos []serverObj.ServerObj, status string, err error) {
	var sip SIP008
	if infos, sip, err = resolveSIP008(raw); err == nil {
		status = getDataUsageStatus(sip.BytesUsed, sip.BytesRemaining)
	} else {
		infos, status, err = resolveByLines(raw)
	}
	return
}

func getDataUsageStatus(bytesUsed, bytesRemaining uint64) (status string) {
	if bytesUsed != 0 {
		status = fmt.Sprintf("Used: %.2f GiB", float64(bytesUsed)/1024/1024/1024)
		if bytesRemaining != 0 {
			status += fmt.Sprintf(" | Remaining: %.2f GiB", float64(bytesRemaining)/1024/1024/1024)
		}
	}
	return
}

type serverWithLatency struct {
	server  configure.Which
	latency int64
	index   int
}

func UpdateSubscription(index int, disconnectIfNecessary bool, filters []string) (err error) {
	subscriptions := configure.GetSubscriptions()
	addr := subscriptions[index].Address
	c := httpClient.GetHttpClientAutomatically()
	resolv.CheckResolvConf()
	subscriptionInfos, status, err := ResolveSubscriptionWithClient(addr, c)
	if err != nil {
		reason := "failed to resolve subscription address: " + err.Error()
		log.Warn("UpdateSubscription: %v: %v", err, subscriptionInfos)
		return fmt.Errorf("UpdateSubscription: %v", reason)
	}

	log.Info("Resolved %d servers from subscription", len(subscriptionInfos))

	// Use stored filters if no new filters provided
	if len(filters) == 0 {
		filters = subscriptions[index].Filters
	}

	// Apply filters if any
	if len(filters) > 0 {
		filteredInfos := make([]serverObj.ServerObj, 0)
		for _, info := range subscriptionInfos {
			if matchesFilters(info, filters) {
				filteredInfos = append(filteredInfos, info)
			}
		}
		subscriptionInfos = filteredInfos
		log.Info("After filtering: %d servers remain", len(subscriptionInfos))
	}

	infoServerRaws := make([]configure.ServerRaw, len(subscriptionInfos))
	css := configure.GetConnectedServers()
	cssAfter := css.Get()
	// serverObj.ServerObj is a pointer(interface), and shouldn't be as a key
	link2Raw := make(map[string]*configure.ServerRaw)
	connectedVmessInfo2CssIndex := make(map[string][]int)
	for i, cs := range css.Get() {
		if cs.TYPE == configure.SubscriptionServerType && cs.Sub == index {
			if sRaw, err := cs.LocateServerRaw(); err != nil {
				return err
			} else {
				link := sRaw.ServerObj.ExportToURL()
				link2Raw[link] = sRaw
				connectedVmessInfo2CssIndex[link] = append(connectedVmessInfo2CssIndex[link], i)
			}
		}
	}

	log.Info("Currently connected servers from this subscription: %d", len(connectedVmessInfo2CssIndex))

	// Create a map of available servers for quick lookup
	availableServers := make(map[string]int)
	for i, info := range subscriptionInfos {
		infoServerRaw := configure.ServerRaw{
			ServerObj: info,
		}
		link := infoServerRaw.ServerObj.ExportToURL()
		availableServers[link] = i + 1
		infoServerRaws[i] = infoServerRaw
	}

	// Track which servers to disconnect
	serversToDisconnect := make([]int, 0)
	// Track which servers to keep
	serversToKeep := make([]int, 0)

	// Check each connected server
	for link, cssIndexes := range connectedVmessInfo2CssIndex {
		if _, exists := availableServers[link]; exists {
			// Server is still available, keep it
			for _, cssIndex := range cssIndexes {
				cssAfter[cssIndex].ID = availableServers[link]
				serversToKeep = append(serversToKeep, cssIndex)
			}
			sr, err := css.Get()[cssIndexes[0]].LocateServerRaw()
			if err != nil {
				log.Warn("Failed to get server info: %v", err)
				continue
			}
			log.Info("Keeping server: %s (ID: %d)", sr.ServerObj.GetName(), availableServers[link])
		} else {
			// Server is no longer available, mark for disconnection
			for _, cssIndex := range cssIndexes {
				serversToDisconnect = append(serversToDisconnect, cssIndex)
			}
			sr, err := css.Get()[cssIndexes[0]].LocateServerRaw()
			if err != nil {
				log.Warn("Failed to get server info: %v", err)
				continue
			}
			log.Info("Server no longer available: %s", sr.ServerObj.GetName())
		}
	}

	// Disconnect unavailable servers
	for _, cssIndex := range serversToDisconnect {
		if disconnectIfNecessary {
			cs := css.Get()[cssIndex]
			sr, err := cs.LocateServerRaw()
			if err != nil {
				log.Warn("Failed to get server info: %v", err)
				continue
			}
			log.Info("Disconnecting server: %s", sr.ServerObj.GetName())
			err = Disconnect(*cs, false)
			if err != nil {
				reason := "failed to disconnect previous server"
				return fmt.Errorf("UpdateSubscription: %v", reason)
			}
		}
	}

	// Calculate how many new servers we need
	currentConnectedCount := len(serversToKeep)
	neededNewServers := 8 - currentConnectedCount

	log.Info("Current connected servers: %d, Need to add: %d", currentConnectedCount, neededNewServers)

	if neededNewServers > 0 {
		// Get all available servers that aren't already connected
		availableForNew := make([]serverWithLatency, 0)
		for i := range subscriptionInfos {
			// Skip if this server is already connected
			isAlreadyConnected := false
			for _, cssIndex := range serversToKeep {
				if cssAfter[cssIndex].ID == i+1 {
					isAlreadyConnected = true
					break
				}
			}
			if !isAlreadyConnected {
				availableForNew = append(availableForNew, serverWithLatency{
					server: configure.Which{
						TYPE: configure.SubscriptionServerType,
						ID:   i + 1,
						Sub:  index,
					},
					index: i + 1,
				})
			}
		}

		log.Info("Found %d new servers to test", len(availableForNew))

		// Ping all available servers
		for i := range availableForNew {
			err := availableForNew[i].server.Ping(5 * time.Second)
			if err != nil {
				log.Warn("Failed to ping server %v: %v", availableForNew[i].server.ID, err)
				continue
			}
			// Parse latency from string like "123ms"
			if latency, err := strconv.ParseInt(strings.TrimSuffix(availableForNew[i].server.Latency, "ms"), 10, 64); err == nil {
				availableForNew[i].latency = latency
				log.Info("Server %v ping result: %vms", availableForNew[i].server.ID, latency)
			}
		}

		// Sort by latency
		sort.Slice(availableForNew, func(i, j int) bool {
			return availableForNew[i].latency < availableForNew[j].latency
		})

		// Connect to top N servers
		for i := 0; i < neededNewServers && i < len(availableForNew); i++ {
			server := availableForNew[i]
			log.Info("Connecting to server %v (latency: %vms)", server.server.ID, server.latency)
			if err = Connect(&server.server); err != nil {
				log.Warn("Failed to connect to server %v: %v", server.server.ID, err)
				continue
			}
			log.Info("Successfully connected to server %v", server.server.ID)
		}
	}

	subscriptions[index].Servers = infoServerRaws
	subscriptions[index].Status = string(touch.NewUpdateStatus())
	subscriptions[index].Info = status
	// Update filters if new ones provided
	if len(filters) > 0 {
		subscriptions[index].Filters = filters
	}
	if err := configure.SetSubscription(index, &subscriptions[index]); err != nil {
		return err
	}

	log.Info("Subscription update completed. Total servers: %d, Connected: %d",
		len(subscriptionInfos), len(serversToKeep)+neededNewServers)

	return nil
}

func ModifySubscriptionRemark(subscription touch.Subscription) (err error) {
	raw := configure.GetSubscription(subscription.ID - 1)
	if raw == nil {
		return fmt.Errorf("failed to find the corresponding subscription")
	}
	raw.Remarks = subscription.Remarks
	raw.Address = subscription.Address
	return configure.SetSubscription(subscription.ID-1, raw)
}
