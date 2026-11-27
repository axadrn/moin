package report

// KnockPort is a well-known port bots knock on: the name a report counts
// its knocks under, the name people know it by, and its port numbers.
type KnockPort struct {
	Name  string
	Label string
	Ports []int
}

// KnockPorts are the ports a daemon counts knocks by, one place for the
// daemon and the site. 80 and 443 are missing on purpose: who comes
// there is a visitor. Knocks on every other port count as KnockOther.
var KnockPorts = []KnockPort{
	{"ssh", "SSH", []int{22}},
	{"telnet", "Telnet", []int{23, 2323}},
	{"ftp", "FTP", []int{21}},
	{"smtp", "SMTP", []int{25}},
	{"dns", "DNS", []int{53}},
	{"rpc", "RPC", []int{111}},
	{"smb", "SMB", []int{139, 445}},
	{"ldap", "LDAP", []int{389}},
	{"rsync", "rsync", []int{873}},
	{"mssql", "SQL Server", []int{1433}},
	{"oracle", "Oracle", []int{1521}},
	{"pptp", "PPTP", []int{1723}},
	{"node-red", "Node-RED", []int{1880}},
	{"mqtt", "MQTT", []int{1883}},
	{"nfs", "NFS", []int{2049}},
	{"docker", "Docker", []int{2375, 2376}},
	{"etcd", "etcd", []int{2379}},
	{"grafana", "Grafana", []int{3000}},
	{"mysql", "MySQL", []int{3306}},
	{"rdp", "Remote Desktop", []int{3389}},
	{"postgres", "Postgres", []int{5432}},
	{"adb", "Android Debug Bridge", []int{5555}},
	{"rabbitmq", "RabbitMQ", []int{5672, 15672}},
	{"vnc", "VNC", []int{5900}},
	{"couchdb", "CouchDB", []int{5984}},
	{"x11", "X11", []int{6000}},
	{"redis", "Redis", []int{6379}},
	{"kubernetes", "Kubernetes", []int{6443}},
	{"http-alt", "Other web ports", []int{8000, 8008, 8080, 8443, 8888}},
	{"influxdb", "InfluxDB", []int{8086}},
	{"minio", "MinIO", []int{9000}},
	{"prometheus", "Prometheus", []int{9090}},
	{"kafka", "Kafka", []int{9092}},
	{"elasticsearch", "Elasticsearch", []int{9200}},
	{"webmin", "Webmin", []int{10000}},
	{"kubelet", "Kubelet", []int{10250}},
	{"memcached", "Memcached", []int{11211}},
	{"ollama", "Ollama", []int{11434}},
	{"mongodb", "MongoDB", []int{27017}},
}

// KnockOther is the name of the knocks on every other port.
const KnockOther = "other"
