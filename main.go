package main

import (
	"flag"
	"fmt"
	"github.com/gorilla/websocket"
	"github.com/sourcegraph/jsonrpc2"
	"mini-sfu/internal/log"
	"mini-sfu/signal"
	"net"
	"net/http"
	"os"

	"mini-sfu/internal/sfu"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	websocketjsonrpc2 "github.com/sourcegraph/jsonrpc2/websocket"
	"github.com/spf13/viper"
)

var (
	conf        = sfu.Config{}
	file        string
	cert        string
	key         string
	addr        string
	metricsAddr string
)

const (
	portRangeLimit = 100
)

func showHelp() {
	fmt.Printf("Usage:%s {params}\n", os.Args[0])
	fmt.Println("      -c {config file}")
	fmt.Println("      -cert {cert file}")
	fmt.Println("      -key {key file}")
	fmt.Println("      -a {listen addr}")
	fmt.Println("      -h (show help info)")
}

func parse() bool {
	flag.StringVar(&file, "c", "config.json", "config file")
	flag.StringVar(&addr, "a", ":8765", "address to use")
	flag.StringVar(&cert, "cert", "", "cert file")
	flag.StringVar(&key, "key", "", "key file")
	flag.StringVar(&metricsAddr, "m", ":8100", "merics to use")
	help := flag.Bool("h", false, "help info")
	flag.Parse()
	if !load() {
		return false
	}

	if *help {
		return false
	}
	return true
}

func load() bool {
	_, err := os.Stat(file)
	if err != nil {
		return false
	}

	viper.SetConfigFile(file)
	viper.SetConfigType("json")

	err = viper.ReadInConfig()
	if err != nil {
		fmt.Printf("config file %s read failed. %v\n", file, err)
		return false
	}
	err = viper.GetViper().Unmarshal(&conf)
	if err != nil {
		fmt.Printf("sfu config file %s loaded failed. %v\n", file, err)
		return false
	}
	fmt.Printf("config ice port len %d, icelite: %d, sdpSemantics: %s, NAT1To1IP:%v\n", len(conf.ICEPortRange), conf.IceLite, conf.SDPSemantics, conf.NAT1To1IPs)
	if len(conf.ICEPortRange) > 2 {
		fmt.Printf("config file %s loaded failed. range port must be [min,max]\n", file)
		return false
	}

	if len(conf.ICEPortRange) != 0 && conf.ICEPortRange[1]-conf.ICEPortRange[0] < portRangeLimit {
		fmt.Printf("config file %s loaded failed. range port must be [min, max] and max - min >= %d\n", file, portRangeLimit)
		return false
	}

	fmt.Printf("config %s load ok!\n", file)
	return true
}

func startMetrics(addr string) {
	m := http.NewServeMux()
	m.Handle("/metrics", promhttp.Handler())
	srv := &http.Server{
		Handler: m,
	}

	metricsLis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Panicf("cannot bind to metrics endpoint %s. err: %s", addr, err)
	}
	log.Infof("Metrics Listening at %s", addr)

	err = srv.Serve(metricsLis)
	if err != nil {
		log.Errorf("debug server stopped. got err: %s", err)
	}
}

func main() {
	if !parse() {
		showHelp()
		os.Exit(-1)
	}
	log.Init("debug")
	log.Infof("--- Starting SFU Node ---")
	s := sfu.NewSFU(conf)
	s.NewDatachannel(sfu.APIChannelLabel)
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}

	http.Handle("/ws", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			panic(err)
		}
		defer c.Close()

		p := signal.NewJSONSignal(sfu.NewPeer(s))
		defer p.Close()

		jc := jsonrpc2.NewConn(r.Context(), websocketjsonrpc2.NewObjectStream(c), p)
		<-jc.DisconnectNotify()
	}))

	go startMetrics(metricsAddr)

	var err error
	if key != "" && cert != "" {
		log.Infof("Listening at https://[%s]", addr)
		err = http.ListenAndServeTLS(addr, cert, key, nil)
	} else {
		log.Infof("Listening at http://[%s]", addr)
		err = http.ListenAndServe(addr, nil)
	}
	if err != nil {
		panic(err)
	}

}
