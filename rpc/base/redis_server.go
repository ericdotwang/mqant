// Copyright 2014 mqant Author. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package defaultrpc

import (
	"github.com/liangdas/mqant/conf"
	"github.com/liangdas/mqant/log"
	"github.com/liangdas/mqant/rpc/pb"
	"github.com/golang/protobuf/proto"
	"github.com/liangdas/mqant/rpc"
	"runtime"
	"github.com/garyburd/redigo/redis"
	"github.com/liangdas/mqant/utils"
)

type RedisServer struct {
	call_chan chan mqrpc.CallInfo
	psc  redis.PubSubConn
	info *conf.Redis
	queueName	string
	done      chan error
}

func NewRedisServer(info *conf.Redis, call_chan chan mqrpc.CallInfo) (*RedisServer, error) {
	var queueName = info.Queue
	var url = info.Uri
	psc := redis.PubSubConn{Conn: utils.GetRedisFactory().GetPool(url).Get()}
	psc.Subscribe(queueName)
	server := new(RedisServer)
	server.call_chan = call_chan
	server.psc = psc
	server.info=info
	server.queueName=queueName
	server.done = make(chan error)
	go server.on_request_handle(server.done)

	return server, nil
	//log.Printf("shutting down")
	//
	//if err := c.Shutdown(); err != nil {
	//	log.Fatalf("error during shutdown: %s", err)
	//}
}

/**
停止接收请求
*/
func (s *RedisServer) StopConsume() error {
	return s.psc.Unsubscribe(s.queueName)
}

/**
注销消息队列
*/
func (s *RedisServer) Shutdown() error {
	s.psc.Unsubscribe(s.queueName)
	return s.psc.Close()
}

func (s *RedisServer) Callback(callinfo mqrpc.CallInfo) error {
	body, _ := s.MarshalResult(callinfo.Result)
	return s.response(callinfo.Props, body)
}

/**
消息应答
*/
func (s *RedisServer) response(props map[string]interface{}, body []byte) error {
	pool:=utils.GetRedisFactory().GetPool(s.info.Uri).Get()
	defer pool.Close()
	var err error
	_, err = pool.Do("PUBLISH", props["reply_to"].(string), body)
	if err != nil {
		log.Warning("Publish: %s", err)
		return err
	}
	return nil
}

/**
接收请求信息
*/
func (s *RedisServer) on_request_handle(done chan error) {
	defer func() {
		if r := recover(); r != nil {
			var rn = ""
			switch r.(type) {

			case string:
				rn = r.(string)
			case error:
				rn = r.(error).Error()
			}
			buf := make([]byte, 1024)
			l := runtime.Stack(buf, false)
			errstr := string(buf[:l])
			log.Error("%s\n ----Stack----\n%s",rn,errstr)
		}
	}()
	for {
		switch v := s.psc.Receive().(type) {
		case redis.Message:
			rpcInfo, err := s.Unmarshal(v.Data)
			if err == nil {
				callInfo:=&mqrpc.CallInfo{
					RpcInfo:*rpcInfo,
				}
				callInfo.Props = map[string]interface{}{
					"reply_to": callInfo.RpcInfo.ReplyTo,
				}

				callInfo.Agent = s //设置代理为AMQPServer

				s.call_chan <- *callInfo
			} else {
				log.Error("error ", err)
			}
		case redis.PMessage:
			rpcInfo, err := s.Unmarshal(v.Data)
			if err == nil {
				callInfo:=&mqrpc.CallInfo{
					RpcInfo:*rpcInfo,
				}
				callInfo.Props = map[string]interface{}{
					"reply_to": callInfo.RpcInfo.ReplyTo,
				}

				callInfo.Agent = s //设置代理为AMQPServer

				s.call_chan <- *callInfo
			} else {
				log.Error("error ", err)
			}
		case redis.Subscription:
			log.Info("%s: %s %d\n", v.Channel, v.Kind, v.Count)
		case error:
			log.Error("on_request_handle",v.Error())
			return
		default:

		}

	}
	log.Debug("finish on_request_handle")
}

func (s *RedisServer) Unmarshal(data []byte) (*rpcpb.RPCInfo, error) {
	//fmt.Println(msg)
	//保存解码后的数据，Value可以为任意数据类型
	var rpcInfo rpcpb.RPCInfo
	err := proto.Unmarshal(data, &rpcInfo)
	if err != nil {
		return nil, err
	} else {
		return &rpcInfo, err
	}

	panic("bug")
}
// goroutine safe
func (s *RedisServer) MarshalResult(resultInfo rpcpb.ResultInfo) ([]byte, error) {
	//log.Error("",map2)
	b, err := proto.Marshal(&resultInfo)
	return b, err
}

