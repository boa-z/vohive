export type ATTemplateItem = {
  label: string
  value: string
}

export type ATTemplateGroup = {
  label: string
  items: ATTemplateItem[]
}

export const AT_TEMPLATES: ATTemplateGroup[] = [
  {
    label: '基础',
    items: [
      { label: '连通性 (AT)', value: 'AT' },
      { label: '模块信息 (ATI)', value: 'ATI' },
      { label: 'IMEI (AT+CGSN)', value: 'AT+CGSN' },
      { label: 'ICCID (AT+QCCID)', value: 'AT+QCCID' },
      { label: 'IMSI (AT+CIMI)', value: 'AT+CIMI' },
      { label: '信号 (AT+CSQ)', value: 'AT+CSQ' },
      { label: '运营商 (AT+COPS?)', value: 'AT+COPS?' },
      { label: '网络模式 (AT+QNWINFO)', value: 'AT+QNWINFO' },
      { label: '注册状态 (AT+CREG?)', value: 'AT+CREG?' },
      { label: '固件版本 (AT+CGMR)', value: 'AT+CGMR' },
      { label: 'APN (AT+CGDCONT?)', value: 'AT+CGDCONT?' },
      { label: '所有信号 (AT+QENG="servingcell")', value: 'AT+QENG="servingcell"' }
    ]
  },
  {
    label: 'SIMCOM',
    items: [
      { label: 'ICCID (AT+CICCID)', value: 'AT+CICCID' },
      { label: '系统信息 (AT+CPSI?)', value: 'AT+CPSI?' },
      { label: '网络模式 (AT+CNSMOD?)', value: 'AT+CNSMOD?' },
      { label: 'USB PID (AT+CUSBPIDSWITCH?)', value: 'AT+CUSBPIDSWITCH?' },
      { label: 'USB Audio (AT+CPCMREG?)', value: 'AT+CPCMREG?' }
    ]
  },
  {
    label: '网络控制',
    items: [
      { label: '飞行模式 ON (AT+CFUN=0)', value: 'AT+CFUN=0' },
      { label: '飞行模式 OFF (AT+CFUN=1)', value: 'AT+CFUN=1' },
      { label: '重启模组 (AT+CFUN=1,1)', value: 'AT+CFUN=1,1' },
      { label: '附着状态 (AT+CGATT?)', value: 'AT+CGATT?' },
      { label: '脱附 (AT+CGATT=0)', value: 'AT+CGATT=0' },
      { label: '附着 (AT+CGATT=1)', value: 'AT+CGATT=1' }
    ]
  },
  {
    label: '漫游服务',
    items: [
      { label: '关闭漫游 (AT+QCFG="roamservice",1,1)', value: 'AT+QCFG="roamservice",1,1' },
      { label: '恢复自动 (AT+QCFG="roamservice",255,1)', value: 'AT+QCFG="roamservice",255,1' }
    ]
  },
  {
    label: 'USBNET / 模式',
    items: [
      { label: '查询模式 (AT+QCFG="usbnet"?)', value: 'AT+QCFG="usbnet"?' },
      { label: '设为 QMI (AT+QCFG="usbnet",0)', value: 'AT+QCFG="usbnet",0' },
      { label: '设为 ECM (AT+QCFG="usbnet",1)', value: 'AT+QCFG="usbnet",1' }
    ]
  },
  {
    label: '短信 / USSD',
    items: [
      { label: '列出短信 (AT+CMGL=4)', value: 'AT+CMGL=4' },
      { label: '读取短信示例 (AT+CMGR=1)', value: 'AT+CMGR=1' },
      { label: '删除所有短信 (AT+CMGD=1,4)', value: 'AT+CMGD=1,4' },
      { label: 'USSD 示例 (AT+CUSD=1,"*100#",15)', value: 'AT+CUSD=1,"*100#",15' }
    ]
  }
]
