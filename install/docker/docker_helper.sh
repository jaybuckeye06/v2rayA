#!/bin/sh
set -x
current_dir=$(pwd)
case "$(arch)" in
    x86_64)
        v2ray_arch="64"
        v2raya_arch="x64"
        ;;
    armv7l)
        v2ray_arch="arm32-v7a"
        v2raya_arch="armv7"
        ;;
    aarch64)
        v2ray_arch="arm64-v8a"
        v2raya_arch="arm64"
        ;;
    riscv64)
        v2ray_arch="riscv64"
        v2raya_arch="riscv64"
        ;;
    *)
        ;;
esac
# 设置 GitHub 仓库信息
OWNER="wasd13579"
REPO="v2rayA"
GITHUB_TOKEN="$GITHUB_TOKEN" 

# 获取最新构建的 ID
BUILD_ID=$(curl -s "https://api.github.com/repos/$OWNER/$REPO/actions/runs" | jq -r '.workflow_runs[0].id')

# 获取最新构建的产物链接
ARTIFACTS=$(curl -s "https://api.github.com/repos/$OWNER/$REPO/actions/runs/$BUILD_ID/artifacts")
ARTIFACT_URL=$(echo "$ARTIFACTS" | jq -r '.artifacts[0].archive_download_url')

# 使用 wget 下载构建产物
echo "Downloading artifact from: $ARTIFACT_URL"
wget --header="Authorization: token $docker" "$ARTIFACT_URL" -O v2raya_linux_x64_latest.zip
# 解压并安装
unzip v2raya_linux_x64_latest.zip -d /usr/local/bin
install /usr/local/bin/v2raya /usr/bin/v2raya

mkdir -p build && cd build || exit
wget https://github.com/v2fly/v2ray-core/releases/latest/download/v2ray-linux-$v2ray_arch.zip
wget https://github.com/XTLS/Xray-core/releases/latest/download/Xray-linux-$v2ray_arch.zip
wget https://github.com/v2rayA/v2rayA/releases/download/vRealv2rayAVersion/v2raya_linux_"$v2raya_arch"_Realv2rayAVersion
unzip v2ray-linux-"$v2ray_arch".zip -d v2ray
install ./v2ray/v2ray /usr/local/bin/v2ray
unzip Xray-linux-"$v2ray_arch".zip -d xray
install ./xray/xray /usr/local/bin/xray

mkdir /usr/local/share/v2raya
ln -s /usr/local/share/v2raya /usr/local/share/v2ray
ln -s /usr/local/share/v2raya /usr/local/share/xray
wget -O /usr/local/share/v2raya/LoyalsoldierSite.dat https://raw.githubusercontent.com/mzz2017/dist-v2ray-rules-dat/master/geosite.dat
wget -O /usr/local/share/v2raya/geosite.dat https://raw.githubusercontent.com/mzz2017/dist-v2ray-rules-dat/master/geosite.dat
wget -O /usr/local/share/v2raya/geoip.dat https://raw.githubusercontent.com/mzz2017/dist-v2ray-rules-dat/master/geoip.dat
cd "$current_dir" || exit
rm -rf build
apk add --no-cache iptables iptables-legacy nftables tzdata
install ./iptables.sh /usr/local/bin/iptables
install ./ip6tables.sh /usr/local/bin/ip6tables
install ./iptables.sh /usr/local/bin/iptables-nft
install ./ip6tables.sh /usr/local/bin/ip6tables-nft
install ./iptables.sh /usr/local/bin/iptables-legacy
install ./ip6tables.sh /usr/local/bin/ip6tables-legacy
