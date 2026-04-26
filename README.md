#### 参考
https://arxiv.org/abs/2602.22942


#### progress
- [x] Turn on the dark theme for the system
- [x] Install Quark App from app store
- [ ] Search result for today’s gold price in USD in  Quark App

#### limit
- 无法识别网页
- 不能输入中文
- 仅支持文本模型

#### config
Create a `.env` file in your working directory with your LLM API credentials:
```env
DEEPSEEK_API_KEY=sk-your-api-key
DEEPSEEK_BASE_URL=https://api.deepseek.com
MODEL_ID=deepseek-v4-flash
```

#### build
for Android from Windows PowerShell
```powershell
$env:CGO_ENABLED = "0"; $env:GOOS = "android"; $env:GOARCH = "arm64"; go build -o ac
```

on bash
```shell
CGO_ENABLED=0 GOOS=android GOARCH=arm64 go build -o ac
```

for native Windows
```powershell
Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED -ErrorAction SilentlyContinue
go build -o ac.exe
```

#### push
```shell
adb push ac /data/local/tmp/
adb push .env /data/local/tmp/
adb shell chmod +x /data/local/tmp/ac
```

#### run
```shell
adb shell
$ cd /data/local/tmp
$ ./ac
```

