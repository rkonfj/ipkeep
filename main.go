package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/vishvananda/netlink"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/concurrency"
	"go.etcd.io/etcd/pkg/transport"
)

func advertiseIp(iface, ip string) error {
	i, err := netlink.LinkByName(iface)
	if err != nil {
		return nil
	}
	addr, err := netlink.ParseAddr(ip + "/32")
	if err != nil {
		return err
	}
	return netlink.AddrAdd(i, addr)
}

func releasingIp(iface, ip string) error {
	i, err := netlink.LinkByName(iface)
	if err != nil {
		return nil
	}
	addr, err := netlink.ParseAddr(ip + "/32")
	if err != nil {
		return err
	}
	return netlink.AddrDel(i, addr)
}

func main() {
	advertisedIp := os.Getenv("ADVERTISE_IP")
	if advertisedIp == "" {
		log.Fatal(errors.New("ADVERTISE_IP is required"))
	}
	iface := os.Getenv("ADVERTISE_IFACE")
	if iface == "" {
		log.Fatal(errors.New("ADVERTISE_IFACE is required"))
	}
	etcdCrt := os.Getenv("ETCD_CERT")
	if etcdCrt == "" {
		etcdCrt = "etcd.crt"
	}
	etcdKey := os.Getenv("ETCD_KEY")
	if etcdKey == "" {
		etcdKey = "etcd.key"
	}
	etcdCaCrt := os.Getenv("ETCD_CA_CERT")
	if etcdCaCrt == "" {
		etcdCaCrt = "etcd-ca.crt"
	}
	tlsInfo := transport.TLSInfo{
		CertFile:      etcdCrt,
		KeyFile:       etcdKey,
		TrustedCAFile: etcdCaCrt,
	}
	tlsConfig, err := tlsInfo.ClientConfig()
	if err != nil {
		log.Fatal(err)
	}
	etcdEndpoints := os.Getenv("ETCD_ENDPOINTS")
	if etcdEndpoints == "" {
		log.Fatal(errors.New("ETCD_ENDPOINTS is required"))
	}
	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   strings.Split(etcdEndpoints, ","),
		DialTimeout: 5 * time.Second,
		TLS:         tlsConfig,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer cli.Close()
	s, err := concurrency.NewSession(cli)
	if err != nil {
		log.Fatal(err)
	}
	defer s.Close()
	elecKey := "/advertise-ip"
	e := concurrency.NewElection(s, elecKey)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	log.Println("attempting to acquire leader lease", elecKey)
	if err := e.Campaign(ctx, advertisedIp); err != nil {
		log.Fatal(err)
	}
	log.Println("advertising ip", advertisedIp)
	err = advertiseIp(iface, advertisedIp)
	if err != nil {
		e.Resign(ctx)
		log.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		log.Println("releasing ip", advertisedIp)
		e.Resign(ctx)
		err := releasingIp(iface, advertisedIp)
		if err != nil {
			log.Println("ERR", err)
		}
		wg.Done()
	}()
	wg.Wait()
}
