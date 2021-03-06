package main

import (
	"flag"
	"net/http"
	"os"
	"runtime"

	"github.com/ximply/redis_exporter/exporter"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
	"net"
)

var (
	redisAddr        = flag.String("redis.addr", getEnv("REDIS_ADDR", ""), "Address of one or more redis nodes, separated by separator")
	redisFile        = flag.String("redis.file", getEnv("REDIS_FILE", ""), "Path to file containing one or more redis nodes, separated by newline. NOTE: mutually exclusive with redis.addr")
	redisPassword    = flag.String("redis.password", getEnv("REDIS_PASSWORD", ""), "Password for one or more redis nodes, separated by separator")
	redisAlias       = flag.String("redis.alias", getEnv("REDIS_ALIAS", ""), "Redis instance alias for one or more redis nodes, separated by separator")
	namespace        = flag.String("namespace", "redis", "Namespace for metrics")
	checkKeys        = flag.String("check-keys", "", "Comma separated list of keys to export value and length/size")
	separator        = flag.String("separator", ",", "separator used to split redis.addr, redis.password and redis.alias into several elements.")
	listenAddress    = flag.String("unix-sock", "/dev/shm/redis_exporter.sock", "Address to listen on for unix sock access and telemetry.")
	metricPath       = flag.String("web.telemetry-path", "/metrics", "Path under which to expose metrics.")
	isDebug          = flag.Bool("debug", false, "Output verbose debug information")
	logFormat        = flag.String("log-format", "txt", "Log format, valid options are txt and json")
	showVersion      = flag.Bool("version", false, "Show version information and exit")
	useCfBindings    = flag.Bool("use-cf-bindings", false, "Use Cloud Foundry service bindings")
	redisMetricsOnly = flag.Bool("redis-only-metrics", false, "Whether to export go runtime metrics also")

	// VERSION, BUILD_DATE, GIT_COMMIT are filled in by the build script
	VERSION     = "<<< filled in by build >>>"
	BUILD_DATE  = "<<< filled in by build >>>"
	COMMIT_SHA1 = "<<< filled in by build >>>"
)

func getEnv(key string, defaultVal string) string {
	if envVal, ok := os.LookupEnv(key); ok {
		return envVal
	}
	return defaultVal
}

func main() {
	flag.Parse()

	switch *logFormat {
	case "json":
		log.SetFormatter(&log.JSONFormatter{})
	default:
		log.SetFormatter(&log.TextFormatter{})
	}
	log.Printf("Redis Metrics Exporter %s    build date: %s    sha1: %s    Go: %s",
		VERSION, BUILD_DATE, COMMIT_SHA1,
		runtime.Version(),
	)
	if *isDebug {
		log.SetLevel(log.DebugLevel)
		log.Debugln("Enabling debug output")
	} else {
		log.SetLevel(log.InfoLevel)
	}

	if *showVersion {
		return
	}

	if *redisFile != "" && *redisAddr != "" {
		log.Fatal("Cannot specify both redis.addr and redis.file")
	}

	var addrs, passwords, aliases []string

	switch {
	case *redisFile != "":
		var err error
		addrs, passwords, aliases, err = exporter.LoadRedisFile(*redisFile)
		if err != nil {
			log.Fatal(err)
		}
	case *useCfBindings:
		addrs, passwords, aliases = exporter.GetCloudFoundryRedisBindings()
	default:
		addrs, passwords, aliases = exporter.LoadRedisArgs(*redisAddr, *redisPassword, *redisAlias, *separator)
	}

	exp, err := exporter.NewRedisExporter(
		exporter.RedisHost{Addrs: addrs, Passwords: passwords, Aliases: aliases},
		*namespace,
		*checkKeys)
	if err != nil {
		log.Fatal(err)
	}

	buildInfo := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "redis_exporter_build_info",
		Help: "redis exporter build_info",
	}, []string{"version", "commit_sha", "build_date", "golang_version"})
	buildInfo.WithLabelValues(VERSION, COMMIT_SHA1, BUILD_DATE, runtime.Version()).Set(1)

	mux := http.NewServeMux()
	if *redisMetricsOnly {
		registry := prometheus.NewRegistry()
		registry.Register(exp)
		registry.Register(buildInfo)
		handler := promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
		mux.Handle(*metricPath, handler)
	} else {
		prometheus.MustRegister(exp)
		prometheus.MustRegister(buildInfo)
		mux.Handle(*metricPath, promhttp.Handler())
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`
<html>
<head><title>Redis Exporter v` + VERSION + `</title></head>
<body>
<h1>Redis Exporter v` + VERSION + `</h1>
<p><a href='` + *metricPath + `'>Metrics</a></p>
</body>
</html>
						`))
	})

	log.Printf("Providing metrics at %s%s", *listenAddress, *metricPath)
	log.Printf("Connecting to redis hosts: %#v", addrs)
	log.Printf("Using alias: %#v", aliases)

	server := http.Server{
		Handler: mux, // http.DefaultServeMux,
	}
	os.Remove(*listenAddress)

	listener, err := net.Listen("unix", *listenAddress)
	if err != nil {
		panic(err)
	}
	server.Serve(listener)
}
