package settings

import (
	"fmt"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

func Init()(err error){
	//指定配置文件名称
	viper.SetConfigName("config")
	//指定配置文件类型
	viper.SetConfigType("yaml")
	//指定查找配置文件的路径(相对路径)
	viper.AddConfigPath("./conf/")

	//读取配置信息
	err = viper.ReadInConfig()
	if err!=nil{
		//读取配置信息失败
		fmt.Printf(
				"viper.ReadInConfig() failed:%v\n",err)
			return
	}

	viper.WatchConfig()
	viper.OnConfigchange(func(in fsnotify.Event)){
		fmt.Println("配置文件修改了")
	}
	return
}