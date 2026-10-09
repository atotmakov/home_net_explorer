function USERDevice(Domain,IpAddr,MacAddr,Port,IpType,DevType,DevStatus,PortType,Time,HostName,IPv4Enabled,IPv6Enabled,DeviceType,UserDevAlias,UserSpecifiedDeviceType,LeaseTimeRemaining)
{
this.Domain = Domain;
this.IpAddr    = (IpAddr.length == 0)?"--":IpAddr;
this.MacAddr= MacAddr;

if(Port=="LAN0" || Port=="SSID0")
{
this.Port  = "--"; 
}
else
{
this.Port  = Port;
}

this.PortID = Port; 

this.PortType= PortType;

this.DevStatus = DevStatus;
this.IpType= IpType;
if(IpType=="Static")
{
  this.DevType="--";
}
else
{
if(DevType=="")
{
this.DevType= "--";
}
else
{
this.DevType= DevType;
}
}
this.Time    = Time;

if(HostName=="")
{
this.HostName= "--";
}
else
{
   this.HostName= HostName;
}

this.IPv4Enabled = IPv4Enabled;
this.IPv6Enabled = IPv6Enabled;
this.DeviceType = DeviceType;
if (UserDevAlias == "")
{
this.UserDevAlias = "--";
}
else
{
this.UserDevAlias = UserDevAlias;
}
this.UserSpecifiedDeviceType = UserSpecifiedDeviceType;
this.LeaseTimeRemaining = LeaseTimeRemaining;
this.instid       = '';
this.IsClickDev   = false;
}

function USERDeviceNew(Domain, IpAddr, MacAddr, RealMacAddr, Port, IpType, DevType, DevStatus, PortType, Time, HostName, IPv4Enabled, IPv6Enabled, DeviceType, UserDevAlias, UserSpecifiedDeviceType, LeaseTimeRemaining) {
this.Domain = Domain;
this.IpAddr = (IpAddr.length == 0)?"--":IpAddr;
this.MacAddr = MacAddr;
this.RealMacAddr = RealMacAddr;

if ((Port=="LAN0") || (Port=="SSID0")) {
this.Port = "--"; 
} else {
this.Port = Port;
}
this.PortID = Port; 
this.PortType = PortType;
this.DevStatus = DevStatus;
this.IpType = IpType;
if (IpType == "Static") {
 this.DevType="--";
} else {
if (DevType == "") {
this.DevType = "--";
} else {
this.DevType = DevType;
}
}
this.Time = Time;

if (HostName == "") {
this.HostName = "--";
} else {
   this.HostName = HostName;
}

this.IPv4Enabled = IPv4Enabled;
this.IPv6Enabled = IPv6Enabled;
this.DeviceType = DeviceType;
if (UserDevAlias == "") {
this.UserDevAlias = "--";
} else {
this.UserDevAlias = UserDevAlias;
}
this.UserSpecifiedDeviceType = UserSpecifiedDeviceType;
this.LeaseTimeRemaining = LeaseTimeRemaining;
this.instid = '';
this.IsClickDev = false;
}

var ProductType = '1';
var isRealmac = '0';
if (ProductType != '2') {
    if (isRealmac == 1) {
        var UserDevinfo = new Array(new USERDeviceNew("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.1","0\x2e0\x2e0\x2e0","00\x3a00\x3a5e\x3a10\x3a00\x3a01","00\x3a00\x3a5e\x3a10\x3a00\x3a01","LAN1","Static","\x2d\x2d","Offline","ETH","16762\x3a33","","0","1","0","","","0"),new USERDeviceNew("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.2","192\x2e168\x2e0\x2e4","00\x3a00\x3a5e\x3a10\x3a00\x3a02","00\x3a00\x3a5e\x3a10\x3a00\x3a02","LAN1","DHCP","vendor\x2dclass","Online","ETH","0\x3a0","host\x2d01","1","1","0","","","65841"),new USERDeviceNew("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.3","192\x2e168\x2e0\x2e3","00\x3a00\x3a5e\x3a10\x3a00\x3a03","00\x3a00\x3a5e\x3a10\x3a00\x3a03","LAN2","DHCP","vendor\x2dclass","Offline","ETH","1277\x3a29","host\x2d02","1","0","0","","","0"),new USERDeviceNew("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.4","192\x2e168\x2e0\x2e5","00\x3a00\x3a5e\x3a10\x3a00\x3a04","00\x3a00\x3a5e\x3a10\x3a00\x3a04","LAN1","DHCP","vendor\x2dclass","Online","ETH","0\x3a0","host\x2d03","1","1","0","","","85962"),new USERDeviceNew("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.5","192\x2e168\x2e0\x2e6","00\x3a00\x3a5e\x3a10\x3a00\x3a05","00\x3a00\x3a5e\x3a10\x3a00\x3a05","LAN1","DHCP","vendor\x2dclass","Online","ETH","60\x3a27","host\x2d04","1","0","0","","","77688"),new USERDeviceNew("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.6","192\x2e168\x2e0\x2e7","00\x3a00\x3a5e\x3a10\x3a00\x3a06","00\x3a00\x3a5e\x3a10\x3a00\x3a06","LAN1","DHCP","","Online","ETH","0\x3a0","host\x2d05","1","1","0","","","61363"),new USERDeviceNew("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.7","192\x2e168\x2e0\x2e8","00\x3a00\x3a5e\x3a10\x3a00\x3a07","00\x3a00\x3a5e\x3a10\x3a00\x3a07","LAN1","DHCP","","Offline","ETH","1137\x3a26","host\x2d06","1","1","0","","","0"),new USERDeviceNew("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.8","192\x2e168\x2e0\x2e9","02\x3a00\x3a5e\x3a10\x3a00\x3a08","02\x3a00\x3a5e\x3a10\x3a00\x3a08","LAN1","DHCP","","Offline","ETH","1071\x3a15","","1","1","0","","","0"),new USERDeviceNew("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.9","192\x2e168\x2e0\x2e10","00\x3a00\x3a5e\x3a10\x3a00\x3a09","00\x3a00\x3a5e\x3a10\x3a00\x3a09","LAN1","DHCP","vendor\x2dclass","Online","ETH","0\x3a0","host\x2d07","1","0","0","","","82425"),new USERDeviceNew("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.10","192\x2e168\x2e0\x2e11","00\x3a00\x3a5e\x3a10\x3a00\x3a0a","00\x3a00\x3a5e\x3a10\x3a00\x3a0a","LAN1","DHCP","","Offline","ETH","0\x3a20","host\x2d08","1","1","0","","","81147"),new USERDeviceNew("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.11","192\x2e168\x2e0\x2e12","02\x3a00\x3a5e\x3a10\x3a00\x3a0b","02\x3a00\x3a5e\x3a10\x3a00\x3a0b","LAN1","DHCP","vendor\x2dclass","Offline","ETH","289\x3a18","host\x2d09","1","1","0","","","0"),new USERDeviceNew("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.12","192\x2e168\x2e0\x2e13","02\x3a00\x3a5e\x3a10\x3a00\x3a0c","02\x3a00\x3a5e\x3a10\x3a00\x3a0c","LAN1","DHCP","vendor\x2dclass","Online","ETH","0\x3a28","host\x2d10","1","1","0","","","59990"),new USERDeviceNew("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.13","192\x2e168\x2e0\x2e14","02\x3a00\x3a5e\x3a10\x3a00\x3a0d","02\x3a00\x3a5e\x3a10\x3a00\x3a0d","LAN1","DHCP","vendor\x2dclass","Online","ETH","0\x3a4","host\x2d11","1","1","0","","","86124"),new USERDeviceNew("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.14","192\x2e168\x2e0\x2e15","00\x3a00\x3a5e\x3a10\x3a00\x3a0e","00\x3a00\x3a5e\x3a10\x3a00\x3a0e","LAN1","DHCP","vendor\x2dclass","Online","ETH","0\x3a28","host\x2d12","1","1","0","","","84286"),new USERDeviceNew("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.15","192\x2e168\x2e0\x2e16","02\x3a00\x3a5e\x3a10\x3a00\x3a0f","02\x3a00\x3a5e\x3a10\x3a00\x3a0f","LAN1","DHCP","","Offline","ETH","1084\x3a22","host\x2d13","1","1","0","","","0"),new USERDeviceNew("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.16","192\x2e168\x2e0\x2e18","00\x3a00\x3a5e\x3a10\x3a00\x3a10","00\x3a00\x3a5e\x3a10\x3a00\x3a10","LAN1","DHCP","vendor\x2dclass","Offline","ETH","144\x3a18","host\x2d14","1","1","0","","","0"),new USERDeviceNew("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.17","192\x2e168\x2e0\x2e19","00\x3a00\x3a5e\x3a10\x3a00\x3a11","00\x3a00\x3a5e\x3a10\x3a00\x3a11","LAN1","DHCP","vendor\x2dclass","Offline","ETH","0\x3a0","host\x2d15","1","0","0","","","86027"),new USERDeviceNew("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.18","192\x2e168\x2e0\x2e17","02\x3a00\x3a5e\x3a10\x3a00\x3a12","02\x3a00\x3a5e\x3a10\x3a00\x3a12","LAN1","DHCP","","Offline","ETH","1084\x3a22","","1","1","0","","","0"),new USERDeviceNew("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.19","192\x2e168\x2e0\x2e20","00\x3a00\x3a5e\x3a10\x3a00\x3a13","00\x3a00\x3a5e\x3a10\x3a00\x3a13","LAN1","DHCP","vendor\x2dclass","Offline","ETH","1111\x3a24","host\x2d16","1","1","0","","","0"),new USERDeviceNew("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.20","192\x2e168\x2e0\x2e21","00\x3a00\x3a5e\x3a10\x3a00\x3a14","00\x3a00\x3a5e\x3a10\x3a00\x3a14","LAN1","DHCP","vendor\x2dclass","Offline","ETH","1085\x3a32","host\x2d17","1","1","0","","","0"),new USERDeviceNew("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.21","192\x2e168\x2e0\x2e22","02\x3a00\x3a5e\x3a10\x3a00\x3a15","02\x3a00\x3a5e\x3a10\x3a00\x3a15","LAN1","DHCP","","Offline","ETH","731\x3a12","","1","1","0","","","0"),new USERDeviceNew("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.22","192\x2e168\x2e0\x2e23","00\x3a00\x3a5e\x3a10\x3a00\x3a16","00\x3a00\x3a5e\x3a10\x3a00\x3a16","LAN1","DHCP","vendor\x2dclass","Offline","ETH","1034\x3a6","host\x2d18","1","1","0","","","0"),new USERDeviceNew("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.23","192\x2e168\x2e0\x2e24","00\x3a00\x3a5e\x3a10\x3a00\x3a17","00\x3a00\x3a5e\x3a10\x3a00\x3a17","LAN2","DHCP","vendor\x2dclass","Online","ETH","0\x3a10","host\x2d19","1","1","0","","","83001"),new USERDeviceNew("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.24","192\x2e168\x2e0\x2e25","00\x3a00\x3a5e\x3a10\x3a00\x3a18","00\x3a00\x3a5e\x3a10\x3a00\x3a18","LAN1","DHCP","vendor\x2dclass","Online","ETH","0\x3a0","host\x2d20","1","1","0","","","65820"),new USERDeviceNew("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.25","192\x2e168\x2e0\x2e26","00\x3a00\x3a5e\x3a10\x3a00\x3a19","00\x3a00\x3a5e\x3a10\x3a00\x3a19","LAN1","DHCP","vendor\x2dclass","Offline","ETH","174\x3a31","host\x2d16","1","1","0","","","0"),new USERDeviceNew("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.26","192\x2e168\x2e0\x2e27","02\x3a00\x3a5e\x3a10\x3a00\x3a1a","02\x3a00\x3a5e\x3a10\x3a00\x3a1a","LAN1","DHCP","vendor\x2dclass","Offline","ETH","76\x3a49","host\x2d21","1","1","0","","","0"),new USERDeviceNew("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.27","192\x2e168\x2e0\x2e28","00\x3a00\x3a5e\x3a10\x3a00\x3a1b","00\x3a00\x3a5e\x3a10\x3a00\x3a1b","LAN1","DHCP","vendor\x2dclass","Online","ETH","0\x3a0","host\x2d22","1","0","0","","","65011"),new USERDeviceNew("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.28","192\x2e168\x2e0\x2e29","02\x3a00\x3a5e\x3a10\x3a00\x3a1c","02\x3a00\x3a5e\x3a10\x3a00\x3a1c","LAN1","DHCP","","Offline","ETH","384\x3a50","","1","1","0","","","0"),new USERDeviceNew("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.29","192\x2e168\x2e0\x2e30","02\x3a00\x3a5e\x3a10\x3a00\x3a1d","02\x3a00\x3a5e\x3a10\x3a00\x3a1d","LAN1","DHCP","","Online","ETH","0\x3a0","","1","1","0","","","78247"),new USERDeviceNew("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.30","192\x2e168\x2e0\x2e31","00\x3a00\x3a5e\x3a10\x3a00\x3a1e","00\x3a00\x3a5e\x3a10\x3a00\x3a1e","LAN1","DHCP","","Online","ETH","0\x3a10","host\x2d23","1","1","0","","","61261"),null);
    } else {
        var UserDevinfo = new Array(new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.1","0\x2e0\x2e0\x2e0","00\x3a00\x3a5e\x3a10\x3a00\x3a01","LAN1","Static","\x2d\x2d","Offline","ETH","16762\x3a33","","0","1","0","","","0"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.2","192\x2e168\x2e0\x2e4","00\x3a00\x3a5e\x3a10\x3a00\x3a02","LAN1","DHCP","vendor\x2dclass","Online","ETH","0\x3a0","host\x2d01","1","1","0","","","65841"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.3","192\x2e168\x2e0\x2e3","00\x3a00\x3a5e\x3a10\x3a00\x3a03","LAN2","DHCP","vendor\x2dclass","Offline","ETH","1277\x3a29","host\x2d02","1","0","0","","","0"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.4","192\x2e168\x2e0\x2e5","00\x3a00\x3a5e\x3a10\x3a00\x3a04","LAN1","DHCP","vendor\x2dclass","Online","ETH","0\x3a0","host\x2d03","1","1","0","","","85962"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.5","192\x2e168\x2e0\x2e6","00\x3a00\x3a5e\x3a10\x3a00\x3a05","LAN1","DHCP","vendor\x2dclass","Online","ETH","60\x3a27","host\x2d04","1","0","0","","","77687"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.6","192\x2e168\x2e0\x2e7","00\x3a00\x3a5e\x3a10\x3a00\x3a06","LAN1","DHCP","","Online","ETH","0\x3a0","host\x2d05","1","1","0","","","61362"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.7","192\x2e168\x2e0\x2e8","00\x3a00\x3a5e\x3a10\x3a00\x3a07","LAN1","DHCP","","Offline","ETH","1137\x3a26","host\x2d06","1","1","0","","","0"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.8","192\x2e168\x2e0\x2e9","02\x3a00\x3a5e\x3a10\x3a00\x3a08","LAN1","DHCP","","Offline","ETH","1071\x3a15","","1","1","0","","","0"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.9","192\x2e168\x2e0\x2e10","00\x3a00\x3a5e\x3a10\x3a00\x3a09","LAN1","DHCP","vendor\x2dclass","Online","ETH","0\x3a0","host\x2d07","1","0","0","","","82424"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.10","192\x2e168\x2e0\x2e11","00\x3a00\x3a5e\x3a10\x3a00\x3a0a","LAN1","DHCP","","Offline","ETH","0\x3a20","host\x2d08","1","1","0","","","81146"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.11","192\x2e168\x2e0\x2e12","02\x3a00\x3a5e\x3a10\x3a00\x3a0b","LAN1","DHCP","vendor\x2dclass","Offline","ETH","289\x3a18","host\x2d09","1","1","0","","","0"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.12","192\x2e168\x2e0\x2e13","02\x3a00\x3a5e\x3a10\x3a00\x3a0c","LAN1","DHCP","vendor\x2dclass","Online","ETH","0\x3a28","host\x2d10","1","1","0","","","59989"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.13","192\x2e168\x2e0\x2e14","02\x3a00\x3a5e\x3a10\x3a00\x3a0d","LAN1","DHCP","vendor\x2dclass","Online","ETH","0\x3a4","host\x2d11","1","1","0","","","86123"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.14","192\x2e168\x2e0\x2e15","00\x3a00\x3a5e\x3a10\x3a00\x3a0e","LAN1","DHCP","vendor\x2dclass","Online","ETH","0\x3a28","host\x2d12","1","1","0","","","84285"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.15","192\x2e168\x2e0\x2e16","02\x3a00\x3a5e\x3a10\x3a00\x3a0f","LAN1","DHCP","","Offline","ETH","1084\x3a22","host\x2d13","1","1","0","","","0"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.16","192\x2e168\x2e0\x2e18","00\x3a00\x3a5e\x3a10\x3a00\x3a10","LAN1","DHCP","vendor\x2dclass","Offline","ETH","144\x3a18","host\x2d14","1","1","0","","","0"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.17","192\x2e168\x2e0\x2e19","00\x3a00\x3a5e\x3a10\x3a00\x3a11","LAN1","DHCP","vendor\x2dclass","Offline","ETH","0\x3a0","host\x2d15","1","0","0","","","86026"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.18","192\x2e168\x2e0\x2e17","02\x3a00\x3a5e\x3a10\x3a00\x3a12","LAN1","DHCP","","Offline","ETH","1084\x3a22","","1","1","0","","","0"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.19","192\x2e168\x2e0\x2e20","00\x3a00\x3a5e\x3a10\x3a00\x3a13","LAN1","DHCP","vendor\x2dclass","Offline","ETH","1111\x3a24","host\x2d16","1","1","0","","","0"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.20","192\x2e168\x2e0\x2e21","00\x3a00\x3a5e\x3a10\x3a00\x3a14","LAN1","DHCP","vendor\x2dclass","Offline","ETH","1085\x3a32","host\x2d17","1","1","0","","","0"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.21","192\x2e168\x2e0\x2e22","02\x3a00\x3a5e\x3a10\x3a00\x3a15","LAN1","DHCP","","Offline","ETH","731\x3a12","","1","1","0","","","0"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.22","192\x2e168\x2e0\x2e23","00\x3a00\x3a5e\x3a10\x3a00\x3a16","LAN1","DHCP","vendor\x2dclass","Offline","ETH","1034\x3a6","host\x2d18","1","1","0","","","0"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.23","192\x2e168\x2e0\x2e24","00\x3a00\x3a5e\x3a10\x3a00\x3a17","LAN2","DHCP","vendor\x2dclass","Online","ETH","0\x3a10","host\x2d19","1","1","0","","","83000"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.24","192\x2e168\x2e0\x2e25","00\x3a00\x3a5e\x3a10\x3a00\x3a18","LAN1","DHCP","vendor\x2dclass","Online","ETH","0\x3a0","host\x2d20","1","1","0","","","65819"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.25","192\x2e168\x2e0\x2e26","00\x3a00\x3a5e\x3a10\x3a00\x3a19","LAN1","DHCP","vendor\x2dclass","Offline","ETH","174\x3a32","host\x2d16","1","1","0","","","0"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.26","192\x2e168\x2e0\x2e27","02\x3a00\x3a5e\x3a10\x3a00\x3a1a","LAN1","DHCP","vendor\x2dclass","Offline","ETH","76\x3a49","host\x2d21","1","1","0","","","0"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.27","192\x2e168\x2e0\x2e28","00\x3a00\x3a5e\x3a10\x3a00\x3a1b","LAN1","DHCP","vendor\x2dclass","Online","ETH","0\x3a0","host\x2d22","1","0","0","","","65010"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.28","192\x2e168\x2e0\x2e29","02\x3a00\x3a5e\x3a10\x3a00\x3a1c","LAN1","DHCP","","Offline","ETH","384\x3a50","","1","1","0","","","0"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.29","192\x2e168\x2e0\x2e30","02\x3a00\x3a5e\x3a10\x3a00\x3a1d","LAN1","DHCP","","Online","ETH","0\x3a0","","1","1","0","","","78246"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.30","192\x2e168\x2e0\x2e31","00\x3a00\x3a5e\x3a10\x3a00\x3a1e","LAN1","DHCP","","Online","ETH","0\x3a10","host\x2d23","1","1","0","","","61260"),null);
    }
} else {
    var UserDevinfo = new Array(new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.1","0\x2e0\x2e0\x2e0","00\x3a00\x3a5e\x3a10\x3a00\x3a01","LAN1","Static","\x2d\x2d","Offline","ETH","16762\x3a33","","0","1","0","","0","0"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.2","192\x2e168\x2e0\x2e4","00\x3a00\x3a5e\x3a10\x3a00\x3a02","LAN1","DHCP","vendor\x2dclass","Online","ETH","0\x3a0","host\x2d01","1","1","0","","0","65840"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.3","192\x2e168\x2e0\x2e3","00\x3a00\x3a5e\x3a10\x3a00\x3a03","LAN2","DHCP","vendor\x2dclass","Offline","ETH","1277\x3a29","host\x2d02","1","0","0","","0","0"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.4","192\x2e168\x2e0\x2e5","00\x3a00\x3a5e\x3a10\x3a00\x3a04","LAN1","DHCP","vendor\x2dclass","Online","ETH","0\x3a0","host\x2d03","1","1","0","","0","85961"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.5","192\x2e168\x2e0\x2e6","00\x3a00\x3a5e\x3a10\x3a00\x3a05","LAN1","DHCP","vendor\x2dclass","Online","ETH","60\x3a27","host\x2d04","1","0","0","","0","77687"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.6","192\x2e168\x2e0\x2e7","00\x3a00\x3a5e\x3a10\x3a00\x3a06","LAN1","DHCP","","Online","ETH","0\x3a0","host\x2d05","1","1","0","","0","61362"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.7","192\x2e168\x2e0\x2e8","00\x3a00\x3a5e\x3a10\x3a00\x3a07","LAN1","DHCP","","Offline","ETH","1137\x3a26","host\x2d06","1","1","0","","0","0"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.8","192\x2e168\x2e0\x2e9","02\x3a00\x3a5e\x3a10\x3a00\x3a08","LAN1","DHCP","","Offline","ETH","1071\x3a15","","1","1","0","","0","0"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.9","192\x2e168\x2e0\x2e10","00\x3a00\x3a5e\x3a10\x3a00\x3a09","LAN1","DHCP","vendor\x2dclass","Online","ETH","0\x3a0","host\x2d07","1","0","0","","0","82424"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.10","192\x2e168\x2e0\x2e11","00\x3a00\x3a5e\x3a10\x3a00\x3a0a","LAN1","DHCP","","Offline","ETH","0\x3a20","host\x2d08","1","1","0","","0","81146"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.11","192\x2e168\x2e0\x2e12","02\x3a00\x3a5e\x3a10\x3a00\x3a0b","LAN1","DHCP","vendor\x2dclass","Offline","ETH","289\x3a18","host\x2d09","1","1","0","","0","0"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.12","192\x2e168\x2e0\x2e13","02\x3a00\x3a5e\x3a10\x3a00\x3a0c","LAN1","DHCP","vendor\x2dclass","Online","ETH","0\x3a28","host\x2d10","1","1","0","","0","59989"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.13","192\x2e168\x2e0\x2e14","02\x3a00\x3a5e\x3a10\x3a00\x3a0d","LAN1","DHCP","vendor\x2dclass","Online","ETH","0\x3a4","host\x2d11","1","1","0","","0","86123"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.14","192\x2e168\x2e0\x2e15","00\x3a00\x3a5e\x3a10\x3a00\x3a0e","LAN1","DHCP","vendor\x2dclass","Online","ETH","0\x3a28","host\x2d12","1","1","0","","0","84285"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.15","192\x2e168\x2e0\x2e16","02\x3a00\x3a5e\x3a10\x3a00\x3a0f","LAN1","DHCP","","Offline","ETH","1084\x3a22","host\x2d13","1","1","0","","0","0"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.16","192\x2e168\x2e0\x2e18","00\x3a00\x3a5e\x3a10\x3a00\x3a10","LAN1","DHCP","vendor\x2dclass","Offline","ETH","144\x3a18","host\x2d14","1","1","0","","0","0"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.17","192\x2e168\x2e0\x2e19","00\x3a00\x3a5e\x3a10\x3a00\x3a11","LAN1","DHCP","vendor\x2dclass","Offline","ETH","0\x3a0","host\x2d15","1","0","0","","0","86026"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.18","192\x2e168\x2e0\x2e17","02\x3a00\x3a5e\x3a10\x3a00\x3a12","LAN1","DHCP","","Offline","ETH","1084\x3a22","","1","1","0","","0","0"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.19","192\x2e168\x2e0\x2e20","00\x3a00\x3a5e\x3a10\x3a00\x3a13","LAN1","DHCP","vendor\x2dclass","Offline","ETH","1111\x3a24","host\x2d16","1","1","0","","0","0"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.20","192\x2e168\x2e0\x2e21","00\x3a00\x3a5e\x3a10\x3a00\x3a14","LAN1","DHCP","vendor\x2dclass","Offline","ETH","1085\x3a32","host\x2d17","1","1","0","","0","0"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.21","192\x2e168\x2e0\x2e22","02\x3a00\x3a5e\x3a10\x3a00\x3a15","LAN1","DHCP","","Offline","ETH","731\x3a12","","1","1","0","","0","0"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.22","192\x2e168\x2e0\x2e23","00\x3a00\x3a5e\x3a10\x3a00\x3a16","LAN1","DHCP","vendor\x2dclass","Offline","ETH","1034\x3a6","host\x2d18","1","1","0","","0","0"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.23","192\x2e168\x2e0\x2e24","00\x3a00\x3a5e\x3a10\x3a00\x3a17","LAN2","DHCP","vendor\x2dclass","Online","ETH","0\x3a10","host\x2d19","1","1","0","","0","83000"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.24","192\x2e168\x2e0\x2e25","00\x3a00\x3a5e\x3a10\x3a00\x3a18","LAN1","DHCP","vendor\x2dclass","Online","ETH","0\x3a0","host\x2d20","1","1","0","","0","65819"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.25","192\x2e168\x2e0\x2e26","00\x3a00\x3a5e\x3a10\x3a00\x3a19","LAN1","DHCP","vendor\x2dclass","Offline","ETH","174\x3a32","host\x2d16","1","1","0","","0","0"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.26","192\x2e168\x2e0\x2e27","02\x3a00\x3a5e\x3a10\x3a00\x3a1a","LAN1","DHCP","vendor\x2dclass","Offline","ETH","76\x3a49","host\x2d21","1","1","0","","0","0"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.27","192\x2e168\x2e0\x2e28","00\x3a00\x3a5e\x3a10\x3a00\x3a1b","LAN1","DHCP","vendor\x2dclass","Online","ETH","0\x3a0","host\x2d22","1","0","0","","0","65010"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.28","192\x2e168\x2e0\x2e29","02\x3a00\x3a5e\x3a10\x3a00\x3a1c","LAN1","DHCP","","Offline","ETH","384\x3a50","","1","1","0","","0","0"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.29","192\x2e168\x2e0\x2e30","02\x3a00\x3a5e\x3a10\x3a00\x3a1d","LAN1","DHCP","","Online","ETH","0\x3a0","","1","1","0","","0","78246"),new USERDevice("InternetGatewayDevice.LANDevice.1.X_HW_UserDev.30","192\x2e168\x2e0\x2e31","00\x3a00\x3a5e\x3a10\x3a00\x3a1e","LAN1","DHCP","","Online","ETH","0\x3a10","host\x2d23","1","1","0","","0","61260"),null);
}

function stWifiWorkingMode(domain,WifiMode,IPAddress,MacAddress)
{
this.domain = domain;
this.WifiMode = WifiMode;
this.IPAddress= IPAddress;
this.MacAddress     = MacAddress;
}

var WifiWorkingModes = new Array(null);
var curCfgModeWord ='MGTS2'; 

if (curCfgModeWord == "TALKTALK2WIFI")
{
for(var i = 0; i < UserDevinfo.length - 1; i++)
{var MACmacthFlag = 0; 
if (UserDevinfo[i].DevStatus.toUpperCase() == "OFFLINE")
{
UserDevinfo[i].Port = "--";
continue;
}

if(UserDevinfo[i].PortType != "WIFI")
{
UserDevinfo[i].Port = "Ethernet";
continue;
}


for(var j = 0;j < WifiWorkingModes.length - 1 ; j++)
{ 
if (UserDevinfo[i].MacAddr.toString().toUpperCase() ==  WifiWorkingModes[j].MacAddress.toString().toUpperCase())
{
UserDevinfo[i].Port = WifiWorkingModes[j].WifiMode;
MACmacthFlag = 1;
break;
}
}

if(MACmacthFlag != 1)
{
for(var k = 0;k < WifiWorkingModes.length - 1 ; k++)
{
if(UserDevinfo[i].IpAddr.toString() == WifiWorkingModes[k].IPAddress.toString())
{
UserDevinfo[i].Port = WifiWorkingModes[k].WifiMode;
MACmacthFlag = 1;
break;
}
}
}
if(MACmacthFlag != 1)
{
UserDevinfo[i].Port = "--";
}
}
}



var UserDevinfoTmp = new Array();
for(var i = 0; i < UserDevinfo.length - 1; i++)
{
var id = UserDevinfo[i].Domain.split(".");
UserDevinfo[i].instid = id[id.length -1];
if("1" == UserDevinfo[i].IPv4Enabled )
{
UserDevinfoTmp.push(UserDevinfo[i]);
}
}
UserDevinfoTmp.push(null);

function GetUserDevInfoList()
{
return UserDevinfoTmp;
}

GetUserDevInfoList();