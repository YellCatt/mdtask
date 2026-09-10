package app

const Usage = `MDTask �?�?Markdown 表格管理任务

用法:
  mdtask [选项] [命令] [参数]

选项（要写在命令之前�?
  -file <路径>     任务 md 文件，默认取 config.yaml 里的 file
  -config <路径>   配置文件，默认依次找 ./config.yaml�?/config.yml
  -no-backup       关闭写前自动备份

状态（md 的状态列里就写这�?emoji，写中文/英文也能识别�?
  �?完成      done      结束态，可归�?  ⏸️ 进行�?   doing
  �?停滞      hold
  🔴 取消      cancel    结束态，可归�?  留空        待办       新建任务的默认状�?
命令:
  ls                       列出任务（无参数时默认执行）
      -s <状�?            按状态筛�?      -p <优先�?          按优先级筛�?      -q <关键�?          搜索全部字段
      -a                   连同「归档」章节一起显�?  add <标题>               新增任务
      -s <状�? -p <优先�? -d <日期> -n <备注>
  show <id>                查看任务详情
  edit <id>                修改任务（只改显式给出的字段�?      -t <标题> -s <状�? -p <优先�? -d <日期> -n <备注>
  mark <id> <状�?         设置状态，状态可�?emoji / 英文 / 中文
  done <id...>             标记 �?完成
  doing <id...>            标记 ⏸️ 进行�?  hold <id...>             标记 �?停滞
  cancel <id...>           标记 🔴 取消
  todo <id...>             清空状态，回到待办
  archive                  把结束态任务搬进�?# 归档」章�?      -before <日期>       只归档截止日期早于该日期�?      -days <N>            只归档截止日期在 N 天之前的
      -all                 �?�?停滞 也一起归�?                           配置 archive.auto=0 后，每次改状态都会自动归�?                           结束态任务；=7 表示只归档截止日期在 7 天前�?  rm <id...>               删除任务
  report                   立刻出一份报告（终端打印 + �?md�?      -type daily           报告类型: daily 日报 / weekly 周报
                            monthly 月报 / yearly 年报
      -date 2026-09-09      参考日期，默认今天
      -last                 统计上一个周期（昨天 / 上周 / 上月 / 去年�?      -open                 生成后打开报告文件
      -no-notify            不弹通知
  daemon                   常驻后台，到点生成日报并弹通知
      -at 21:00,05:00      日报时间（默�?21:00 �?05:00�?      -interval 60         扫描 md 变化的间隔（秒）
      -open                生成后顺便打开报告文件
      -once                立刻出一份日报并退出（调试用）
                           周报 / 月报 / 年报�?config.yaml �?                           report.weekly / monthly / yearly 里配�?                           到点跟日报一起出
  mail                     把本�?IP 信息发到邮箱
      -to a@b.com          收件人，默认取配置里�?      -subject "标题"       默认自动生成
      -body "附加内容"      追加�?IP 信息后面
      -dry                 只打印邮件内容，不真�?                           邮箱参数�?config.yaml �?mail 段里�?  install                  �?daemon 装进开机启动项
  uninstall                移除开机启动项
  init                     生成一份带注释的默�?config.yaml
      -force               已存在时覆盖
                           程序启动时发现没有配置文件会自己生成一份，
                           一般不用手动跑这个
  path                     打印 md 文件的绝对路�?  open                     用系统默认程序打开 md 文件
  help                     显示本帮�?
示例:
  mdtask add "�?Go 小项�? -p high -d 2026-09-15 -n "基于 md 文件驱动"
  mdtask mark 2 ⏸️
  mdtask done 2
  mdtask archive -days 7
  mdtask ls -a
  mdtask report                     出今天的日报
  mdtask report weekly -last        出上周的周报
  mdtask report monthly -date 2026-08-01
  mdtask report yearly
  mdtask mail -dry                先看看要发什�?  mdtask mail -to me@example.com
`


