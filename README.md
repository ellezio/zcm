# Zabbix connection monitoring agent
ZCM (Zabbix connection monitoring) is an agent which sends requests on provided endpoints with authorization and data (`json` or `encoded form data`) if `POST` method choosen. Zabbix server/proxy (veriosn 7.0 and higher) can collect data like **response time**, **status** and **status code**.

## Quickstart
- Pull image from **[DockerHub](https://hub.docker.com/r/ellezio/zcm)** and run with **[monitoring targets](#monitoring-targets)** file
```
docker run \
    -p 10050:10050 \
    -v ./monitoring-targets.yml:/monitoring-targets.yml \
    --name zcm \
    ellezio/zcm:0.1.0
```

- Or build docker image from source and run with **[monitoring targets](#monitoring-targets)** file
```
docker build --target release --tag zcm .
docker run \
    -p 10050:10050 \
    -v ./monitoring-targets.yml:/monitoring-targets.yml \
    --name zcm \
    zcm
```

- Or get source and build with `go` and run with **[monitoring targets](#monitoring-targets)** file in same directory
```
go build -o zcm ./cmd/zcm
```

## Available cli arguments
- --targets-file (short -t) *<[monitoring-targets](#monitoring-targets)-file-path>*

## Monitoring targets
Structure of monitoring-targets.yml file
```yaml
some-name: # zabbix collects data by this name + parameter
  url: http://some-url.some
  method: POST # optional; default GET, available: POST or GET
  interval: 10000 # optional; default 10000 in milliseconds
  authorization: # optional
    type: Basic # currently only Basic supports username and password
    username: user # not allowed when token provided
    password: passwd # not allowed when token provided
    token: sometoken # not allowed when username or password provided
  json: | # json available if method is POST and form-data field is not present
    {
      "Key": "Val"
    }
  form-data: # form-data available if method is POST and json field is not present
    key: val
```

For url and all authorization fields getting data from environment variable is supported
```yaml
# ...
url: http://{env:IP}:{env:PORT}
authorization:
    type: "{env:AUTH_TYPE}"
    username: "{env:AUTH_USER}"
    password: "{env:AUTH_PASSWD}"
    token: "{env:AUTH_TOKEN}"
# ...
```

## Target's parameters
To get specific data from item append to item key a "." with one of parameters.
- `responseTime` - last response time or if currently executing request is pending longer than last response time, get it's value
- `statusCode` - integer representing last response status code
- `status` - code + description e.g. *200 OK*
