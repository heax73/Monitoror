package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net"
	"time"
	"github.com/shirou/gopsutil/v3/cpu"
	_ "github.com/go-sql-driver/mysql"
	"google.golang.org/grpc"
	pb "github.com/send"
)

const (
	USERNAME = "root"
	PASSWORD = "mysql991004"
	NETWORK  = "tcp"
	SERVER   = "localhost"
	MYSQLPORT = 3306
	DATABASE = "Serverinfo"
	RPCPORT = ":5050"
)

type cpuInfo struct {
	cpuid int32
	pcore int
	lcore int
	occupancy float64
	Mhz float64
	CacheSize int32
}

type SendService struct {
	pb.UnimplementedSenderServer // 被调用的服务端接口
}

// 方案1.rpc函数内部调用visorCpu()
// 方案2.main()中, rpc函数接收visorCpu()结果
// 方案3.visorCpu_RPC()
// 这里检测到的cpu状况是server的还是client的？(server的)
func (s *SendService) SendData (ctx context.Context, in *pb.SendRequest) (*pb.SendResponse, error) {
	// 连接Mysql
	dSN := fmt.Sprintf("%s:%s@%s(%s:%d)/%s",USERNAME,PASSWORD,NETWORK,SERVER,MYSQLPORT,DATABASE)
	db, err := sql.Open("mysql", dSN)
	defer db.Close()
	if err != nil {
		log.Println("open mysql failed, ", err)
	}
	err = db.Ping() // 尝试ping
	if err != nil {
		log.Println("ping failed, ", err)
	}
	// 查询mysql
	res, err := db.Query("SELECT * from cpuinfo where cpu_id = (SELECT max(cpu_id) FROM cpuinfo)")
	defer res.Close()
	if err != nil {
		log.Fatalf("Query failed: %v", err) 
	}
	var resp cpuInfo
	for res.Next() {
		err := res.Scan(&resp.cpuid, &resp.pcore, &resp.lcore, &resp.occupancy, &resp.Mhz, &resp.CacheSize)
		if err != nil {
			log.Fatalf("Query Scan failed: %v", err)
		}
	}
	return &pb.SendResponse {
		Cpuid: resp.cpuid,
		Pcore: int32(resp.pcore),
		Lcore: int32(resp.lcore),
		Occupancy: resp.occupancy,
		Mhz: resp.Mhz,
		CacheSize: resp.CacheSize,
		Receiver: in.GetHostname(),
	}, nil
}

func main() {
	// 连接mysql
	dSN := fmt.Sprintf("%s:%s@%s(%s:%d)/%s",USERNAME,PASSWORD,NETWORK,SERVER,MYSQLPORT,DATABASE)
	db, err := sql.Open("mysql", dSN)
	defer db.Close()
	if err != nil {
		log.Println("open mysql failed, ", err)
	}
	err = db.Ping() // 尝试ping
	if err != nil {
		log.Println("ping failed, ", err)
	}
	// 协程每5秒采集cpu数据,并把数据insert到mysql
	go func() {
		for {
			cpuCurrentStat := visorCPU() // 采集cpu数据
			insertData(db, cpuCurrentStat) // 插入cpu数据到mysql
		}
	}()

	// rpc
	// 创建listen(), 监听tcp端口
	lis, err := net.Listen("tcp", RPCPORT)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	server := grpc.NewServer() // 创建gRPC Server对象
	// 将SendService注册到Server的内部注册中心
	// 接收到请求时, 可通过服务发现, 发现接口并转接进行处理
	pb.RegisterSenderServer(server, &SendService{})
	if err := server.Serve(lis); err != nil { // server开始accept, 直到stop
		log.Fatalf("failed to serve: %v", err)
	}
}

func visorCPU() cpuInfo {
	pcore, _ := cpu.Counts(false)
	lcore, _ := cpu.Counts(true)
	//fmt.Printf("物理核数: %v, 逻辑核数: %v \n", physicCore, logicCore)

	//seconds := 1
	occupancy, _ := cpu.Percent(time.Second, false) // false, 总cpu使用率
	//fmt.Printf("cpu总占用率: %v \n", cpuOccupancy[0])

	cpuStat, _ := cpu.Info()
	//fmt.Printf("%v, %v \n", cpuStat[0].Mhz, cpuStat[0].CacheSize)

	return cpuInfo{
		cpuid: cpuStat[0].CPU,
		pcore: pcore,
		lcore: lcore,
		occupancy: occupancy[0],
		Mhz: cpuStat[0].Mhz,
		CacheSize: cpuStat[0].CacheSize,
	}
}

func insertData(db *sql.DB, cpuinfo cpuInfo) {
	result, err := db.Exec("insert into cpuinfo(physical_core, logical_core, occupancy, mhz, cachesize) values(?,?,?,?,?)", cpuinfo.pcore, cpuinfo.lcore, cpuinfo.occupancy, cpuinfo.Mhz, cpuinfo.CacheSize)
	if err != nil {
		log.Println("exec failed, ", err)
		return
	}
	id, err := result.LastInsertId()
	if err != nil {
		log.Println("exec failed, ", err)
		return
	}
	fmt.Println("insert succ: ", id)
}

