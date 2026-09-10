package main

const usage = `MDTask — 用 Markdown 表格管理任务

用法:
  mdtask [选项] [命令] [参数]

选项（要写在命令之前）:
  -file <路径>     任务 md 文件，默认取 config.yaml 里的 file
  -config <路径>   配置文件，默认依次找 ./config.yaml、./config.yml
  -no-backup       关闭写前自动备份

状态（md 的状态列里就写这些 emoji，写中文/英文也能识别）:
  ✅ 完成      done      结束态，可归档
  ⏸️ 进行中    doing
  ❌ 停滞      hold
  🔴 取消      cancel    结束态，可归档
  留空        待办       新建任务的默认状态

命令:
  ls                       列出任务（无参数时默认执行）
      -s <状态>            按状态筛选
      -p <优先级>          按优先级筛选
      -q <关键词>          搜索全部字段
      -a                   连同「归档」章节一起显示
  add <标题>               新增任务
      -s <状态> -p <优先级> -d <日期> -n <备注>
  show <id>                查看任务详情
  edit <id>                修改任务（只改显式给出的字段）
      -t <标题> -s <状态> -p <优先级> -d <日期> -n <备注>
  mark <id> <状态>         设置状态，状态可写 emoji / 英文 / 中文
  done <id...>             标记 ✅ 完成
  doing <id...>            标记 ⏸️ 进行中
  hold <id...>             标记 ❌ 停滞
  cancel <id...>           标记 🔴 取消
  todo <id...>             清空状态，回到待办
  archive                  把结束态任务搬进「## 归档」章节
      -before <日期>       只归档截止日期早于该日期的
      -days <N>            只归档截止日期在 N 天之前的
      -all                 连 ❌ 停滞 也一起归档
                           配置 archive.auto=0 后，每次改状态都会自动归档
                           结束态任务；=7 表示只归档截止日期在 7 天前的
  rm <id...>               删除任务
  daemon                   常驻后台，到点生成日报并弹通知
      -at 21:00,05:00      日报时间（默认 21:00 与 05:00）
      -interval 60         扫描 md 变化的间隔（秒）
      -open                生成后顺便打开日报文件
      -once                立刻出一份日报并退出（调试用）
  install                  把 daemon 装进开机启动项
  uninstall                移除开机启动项
  init                     在当前目录生成一份带注释的默认 config.yaml
      -force               已存在时覆盖
  path                     打印 md 文件的绝对路径
  open                     用系统默认程序打开 md 文件
  help                     显示本帮助

示例:
  mdtask add "写 Go 小项目" -p high -d 2026-09-15 -n "基于 md 文件驱动"
  mdtask mark 2 ⏸️
  mdtask done 2
  mdtask archive -days 7
  mdtask ls -a
`
