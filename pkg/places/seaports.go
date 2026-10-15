package places

// seaports are large cargo sea ports (UN/LOCODE): the world's busiest
// container ports and the main gateway port of most coastal countries.
// The calculator moves a sea leg that starts or ends inland to one of
// them (calc.KindSeaPort).
var seaports = map[string]bool{}

func init() {
	for _, c := range []string{
		// East and Southeast Asia
		"CNSHG", "CNNBG", "CNSZP", "CNYTN", "CNSHK", "CNQIN", "CNTXG", "CNXMG", "CNDAL", "CNGZG", "CNNSA",
		"CNLYG", "CNFZH", "CNRZH", "CNYIK", "HKHKG", "TWKHH", "TWTPE", "TWKEL", "KRPUS", "KRINC", "KRKAN",
		"JPTYO", "JPYOK", "JPUKB", "JPOSA", "JPNGO", "SGSIN", "MYPKG", "MYTPP", "MYPEN", "THLCH", "THBKK",
		"VNSGN", "VNCMT", "VNHPH", "IDJKT", "IDSUB", "IDBLW", "PHMNL", "KHKOS", "MMRGN", "RUVVO",
		// South Asia and the Middle East
		"LKCMB", "INNSA", "INMUN", "INMAA", "INCCU", "INVTZ", "INCOK", "INPAV", "INHZA", "PKKHI", "PKBQM",
		"BDCGP", "AEJEA", "AEKHL", "OMSLL", "OMSOH", "SAJED", "SADMM", "QAHMD", "KWSAA", "IRBND", "BHKBS",
		"ILHFA", "ILASH", "JOAQJ", "IQUQR",
		// Europe
		"NLRTM", "BEANR", "BEZEE", "DEHAM", "DEBRV", "DEWVN", "FRLEH", "FRFOS", "FRMRS", "FRDKK", "GBFXT",
		"GBSOU", "GBLGP", "GBLIV", "GBTIL", "ESVLC", "ESALG", "ESBCN", "ESBIO", "PTSIE", "PTLIS", "PTLEI",
		"ITGOA", "ITSPE", "ITGIT", "ITTRS", "ITVCE", "ITLIV", "ITNAP", "ITSAL", "SIKOP", "HRRJK", "GRPIR",
		"GRSKG", "TRAMR", "TRMER", "TRIZM", "TRIZT", "ROCND", "BGVAR", "PLGDN", "PLGDY", "DKAAR", "SEGOT",
		"NOOSL", "FIHEL", "LTKLJ", "LVRIX", "EETLL", "RULED", "RUNVS", "UAODS", "MTMAR", "IEDUB", "CYLMS",
		// Africa
		"EGPSD", "EGALY", "EGSOK", "MAPTM", "MACAS", "DZALG", "TNRDS", "ZADUR", "ZACPT", "ZAPLZ", "ZAZBA",
		"KEMBA", "TZDAR", "DJJIB", "SDPZU", "NGAPP", "NGTIN", "GHTEM", "CIABJ", "SNDKR", "TGLFW", "BJCOO",
		"CMDLA", "AOLAD", "MZMPM", "NAWVB", "MUPLU",
		// The Americas
		"USLAX", "USLGB", "USOAK", "USSEA", "USTIW", "USNYC", "USSAV", "USCHS", "USORF", "USHOU", "USMIA",
		"USJAX", "USBAL", "USPHL", "USMOB", "USMSY", "USBOS", "USPEF", "CAVAN", "CAPRR", "CAMTR", "CAHAL",
		"MXZLO", "MXLZC", "MXVER", "MXATM", "PABLB", "PAMIT", "PAONX", "CRLIO", "GTSTC", "HNPCR", "COCTG",
		"COBUN", "ECGYE", "PECLL", "CLSAI", "CLVAP", "BRSSZ", "BRPNG", "BRRIG", "BRITJ", "BRSSA", "BRRIO",
		"BRSUA", "BRPEC", "ARBUE", "UYMVD", "JMKIN", "DOCAU", "BSFPO", "TTPOS",
		// Oceania
		"AUMEL", "AUSYD", "AUBNE", "AUFRE", "AUADL", "NZAKL", "NZTRG",
	} {
		seaports[c] = true
	}
}
