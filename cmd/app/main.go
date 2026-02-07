package main

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"crypto/tls"
	"fmt"
	"time"
	"strings"
	htmlpkg "html"
	"encoding/json"
	"log"
	"io"
	"strconv"
	"sort"
	"sync"
	"os"
)

// Debug flag controlled by ENV APP_DEBUG (true/1/yes to enable)
var debugApi bool = false
// CHECKTRENDTIME controls how far back getLastTrend queries trends.
// Format examples: 30d, 1d, 12h, 10m (days/hours/minutes). Defaults to 30d.
var checkTrendDurationSeconds int64 = 30 * 24 * 60 * 60

func parseCheckTrendEnv() {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("CHECKTRENDTIME")))
	if v == "" {
		return
	}
	// last char is unit
	n := len(v)
	unit := v[n-1]
	numPart := v[:n-1]
	// if unit is digit (no unit supplied), treat whole as minutes
	       if unit >= '0' && unit <= '9' {
		       // entire string is numeric -> minutes
		       if val, err := strconv.Atoi(v); err == nil {
			       checkTrendDurationSeconds = int64(val) * 60
			       log.Printf("[DEBUG] CHECKTRENDTIME parsed as %d minutes", val)
			       return
		       }
		       log.Printf("[WARN] CHECKTRENDTIME invalid: %s", v)
		       return
	       }
	val, err := strconv.Atoi(numPart)
	if err != nil {
		log.Printf("[WARN] CHECKTRENDTIME invalid number: %s", v)
		return
	}
	       switch unit {
	       case 'd':
		       checkTrendDurationSeconds = int64(val) * 24 * 60 * 60
	       case 'h':
		       checkTrendDurationSeconds = int64(val) * 60 * 60
	       case 'm':
		       checkTrendDurationSeconds = int64(val) * 60
	       default:
		       log.Printf("[WARN] CHECKTRENDTIME invalid unit '%c' in %s", unit, v)
		       return
	       }
	log.Printf("[DEBUG] CHECKTRENDTIME set to %s -> %d seconds", v, checkTrendDurationSeconds)

}

// Reusable HTTP client to improve performance (connection reuse)
var httpClient *http.Client

// Simple cache for item lookups: key is key+"|"+hostid -> map[string]interface{}
var itemLookupCache sync.Map

// Optional progress callback used to send textual progress updates back to caller
var progressCb func(string)

func initHttpClient() {
	if httpClient != nil {
		return
	}
	tr := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	httpClient = &http.Client{Transport: tr, Timeout: 20 * time.Second}
}

// Função para coletar e gerar requisições ao Zabbix API
func zabbixApiRequest(apiUrl, token, method string, params interface{}) (map[string]interface{}, error) {
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"method": method,
		"params": params,
		"id": 1,
	}
	if token != "" {
		req["auth"] = token
	}
	reqBytes, _ := json.Marshal(req)
	if debugApi {
		log.Printf("[ZABBIX DEBUG] Request %s -> %s", method, string(reqBytes))
	}
	if httpClient == nil {
		initHttpClient()
	}
	var resp *http.Response
	start := time.Now()
	resp, err := httpClient.Post(apiUrl, "application/json", strings.NewReader(string(reqBytes)))
	if err != nil {
		log.Printf("[ZABBIX API] %s failed after %s: %v", method, time.Since(start), err)
		return nil, err
	}
	defer resp.Body.Close()
	bodyBytes, _ := io.ReadAll(resp.Body)
	if debugApi {
		b := string(bodyBytes)
		if len(b) > 4096 { b = b[:4096] + "...(truncated)" }
		log.Printf("[ZABBIX DEBUG] Response %s <- status=%s body=%s", method, resp.Status, b)
	}
	var result map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		log.Printf("[ZABBIX API] %s unmarshal failed after %s: %v", method, time.Since(start), err)
		return nil, err
	}
	if errObj, ok := result["error"]; ok {
		log.Printf("[ZABBIX API] %s returned error after %s: %v", method, time.Since(start), errObj)
		return nil, fmt.Errorf("API error: %v", errObj)
	}
	log.Printf("[ZABBIX API] %s completed in %s, status=%s", method, time.Since(start), resp.Status)
	return result, nil
}

// getItemByKey busca item(s) pelo key_ (opcional hostid). Retorna o primeiro item ou nil.
func getItemByKey(apiUrl, token, key, hostid string) (map[string]interface{}, error) {
	// check cache first
	cacheKey := key + "|" + hostid
	if v, ok := itemLookupCache.Load(cacheKey); ok {
		if m, ok2 := v.(map[string]interface{}); ok2 {
			return m, nil
		}
	}

	params := map[string]interface{}{
		"output": []string{"itemid", "hostid", "name", "key_", "value_type"},
		// use exact filter for key_ to avoid partial matches and extra work
		"filter": map[string]interface{}{"key_": key},
		"limit": 1,
	}
	if hostid != "" {
		params["hostids"] = hostid
	}
	resp, err := zabbixApiRequest(apiUrl, token, "item.get", params)
	if err != nil { return nil, err }
	if r, ok := resp["result"]; ok {
		arr, _ := r.([]interface{})
		if len(arr) > 0 {
			m := arr[0].(map[string]interface{})
			itemLookupCache.Store(cacheKey, m)
			return m, nil
		}
	}
	return nil, nil
}

// getLastHistoryValue pega o último valor de history para um itemid, usando historyType (0=float,3=int,1=char etc.)
func getLastHistoryValue(apiUrl, token, itemid string, historyType int) (string, error) {
	params := map[string]interface{}{
		"output": "extend",
		"history": historyType,
		"itemids": itemid,
		"sortfield": "clock",
		"sortorder": "DESC",
		"limit": 1,
	}
	resp, err := zabbixApiRequest(apiUrl, token, "history.get", params)
	if err != nil { return "", err }
	if r, ok := resp["result"]; ok {
		arr, _ := r.([]interface{})
		if len(arr) > 0 {
			hist := arr[0].(map[string]interface{})
			return fmt.Sprintf("%v", hist["value"]), nil
		}
	}
	return "", nil
}

// getLastTrend busca o último registro em trend para um itemid no intervalo now - days
func getLastTrend(apiUrl, token, itemid string, days int) (map[string]interface{}, error) {
	now := time.Now().Unix()
	// compute 'from' based on CHECKTRENDTIME if provided, otherwise use days param (in days)
	var from int64
	if checkTrendDurationSeconds > 0 {
		from = now - checkTrendDurationSeconds
	} else {
		from = now - int64(days*24*60*60)
	}
	params := map[string]interface{}{
		"output": []string{"itemid", "clock", "value_min", "value_avg", "value_max"},
		"itemids": []string{itemid},
		"limit": 1,
		"time_from": from,
		"time_to": now,
	}
	resp, err := zabbixApiRequest(apiUrl, token, "trend.get", params)
	if err != nil { return nil, err }
	if r, ok := resp["result"]; ok {
		arr, _ := r.([]interface{})
		if len(arr) > 0 {
			return arr[0].(map[string]interface{}), nil
		}
	}
	return nil, nil
}

// getProxyCount returns the total number of proxies configured in Zabbix (countOutput)
func getProxyCount(apiUrl, token string) (int, error) {
	params := map[string]interface{}{
		"output": "extend",
		"countOutput": true,
	}
	resp, err := zabbixApiRequest(apiUrl, token, "proxy.get", params)
	if err != nil { return 0, err }
	if r, ok := resp["result"]; ok {
		switch v := r.(type) {
		case float64:
			return int(v), nil
		case int:
			return v, nil
		default:
			if v2, err := strconv.Atoi(fmt.Sprintf("%v", v)); err == nil { return v2, nil }
		}
	}
	return 0, nil
}

// getProxies returns the list of proxies with full details (output extend)
func getProxies(apiUrl, token string) ([]map[string]interface{}, error) {
	params := map[string]interface{}{
		"output": "extend",
	}
	resp, err := zabbixApiRequest(apiUrl, token, "proxy.get", params)
	if err != nil { return nil, err }
	if r, ok := resp["result"]; ok {
		arr, _ := r.([]interface{})
		out := []map[string]interface{}{}
		for _, it := range arr {
			if m, ok := it.(map[string]interface{}); ok {
				out = append(out, m)
			}
		}
		return out, nil
	}
	return nil, nil
}

func generateZabbixReport(url, token string) (string, error) {
		nItensNaoSuportados := "-"
	log.Printf("[DEBUG] Iniciando coleta Zabbix: url=%s", url)
	apiUrl := url
	if apiUrl[len(apiUrl)-1] != '/' {
		apiUrl += "/api_jsonrpc.php"
	} else {
		apiUrl += "api_jsonrpc.php"
	}

	// Detect Zabbix API version early (apiinfo.version)
	// Use an empty auth when querying version so detection works even with invalid token
	zabbixVersion := ""
	verResp, err := zabbixApiRequest(apiUrl, "", "apiinfo.version", []interface{}{})
	if err == nil {
		if r, ok := verResp["result"]; ok {
			zabbixVersion = fmt.Sprintf("%v", r)
		}
	}
	// parse major/minor
	majorV := 0
	minorV := 0
	if zabbixVersion != "" {
		parts := strings.SplitN(zabbixVersion, ".", 3)
		if len(parts) > 0 {
			if v, err := strconv.Atoi(parts[0]); err == nil { majorV = v }
		}
		if len(parts) > 1 {
			if v, err := strconv.Atoi(parts[1]); err == nil { minorV = v }
		}
	}
	// minorV may be used elsewhere; avoid unused variable compile error
	_ = minorV
	// Consulta quantidade de itens não suportados
	itensNaoSuportadosResp, err := zabbixApiRequest(apiUrl, token, "item.get", map[string]interface{}{
		"output": "extend",
		"filter": map[string]interface{}{ "state": 1, "status": 0 },
		"monitored": true,
		"countOutput": true,
	})
	if err == nil {
		nItensNaoSuportados = fmt.Sprintf("%v", itensNaoSuportadosResp["result"])
	}
	// 1. Usuários
	if progressCb != nil { progressCb("Coletando informações de Usuários...") }
	userResp, err := zabbixApiRequest(apiUrl, token, "user.get", map[string]interface{}{ "output": "userid" })
	if err != nil { return "", err }
	users := userResp["result"].([]interface{})
	nUsers := len(users)

	// 2. NVPS (Required server performance, new values per second)
	if progressCb != nil { progressCb("Coletando informações de NVPS (Required performance)...") }
	// Flow: item.get with hostid + key_ -> if exists, history.get(last) using item's value_type
	nvps := "N/A"
	requiredHost := os.Getenv("ZABBIX_SERVER_HOSTID")
	if requiredHost == "" {
		log.Printf("[DEBUG] ZABBIX_SERVER_HOSTID not set; searching without hostid for zabbix[requiredperformance]")
	} else {
		log.Printf("[DEBUG] ZABBIX_SERVER_HOSTID=%s will be used for item.get", requiredHost)
	}
	if item, err := getItemByKey(apiUrl, token, "zabbix[requiredperformance]", requiredHost); err == nil {
		if item != nil {
			// log item details for debugging
			log.Printf("[DEBUG] Found requiredperformance item: itemid=%v hostid=%v value_type=%v key=%v", item["itemid"], item["hostid"], item["value_type"], item["key_"])
			itemid := fmt.Sprintf("%v", item["itemid"])
			// determine history type from value_type (fallback to 0)
			histType := 0
			if vt, ok := item["value_type"]; ok {
				switch v := vt.(type) {
				case float64:
					histType = int(v)
				case int:
					histType = v
				default:
					if v2, err := strconv.Atoi(fmt.Sprintf("%v", v)); err == nil { histType = v2 }
				}
			}
			log.Printf("[DEBUG] Using history type %d for itemid %s", histType, itemid)
			if val, err := getLastHistoryValue(apiUrl, token, itemid, histType); err == nil {
				if val != "" {
					// try to format as float with 2 decimals
					if f, ferr := strconv.ParseFloat(strings.TrimSpace(val), 64); ferr == nil {
						nvps = fmt.Sprintf("%.2f", f)
					} else {
						nvps = val
					}
				} else {
					nvps = "Sem histórico"
				}
			} else {
				log.Printf("[ERROR] history.get failed for itemid %s: %v", itemid, err)
				nvps = "Erro ao ler histórico"
			}
		} else {
			nvps = "Criar chave zabbix[requiredperformance] no Template do Zabbix"
		}
	} else {
		log.Printf("[ERROR] item.get failed for requiredperformance: %v", err)
		nvps = "Erro ao procurar item"
	}

	// 3. Hosts
	if progressCb != nil { progressCb("Coletando informações de Hosts...") }

    
	hostsResp, err := zabbixApiRequest(apiUrl, token, "host.get", map[string]interface{}{ "output": "hostid" })
	if err != nil { return "", err }
	totalHosts := hostsResp["result"].([]interface{})
	nTotalHosts := len(totalHosts)

	enabledResp, err := zabbixApiRequest(apiUrl, token, "host.get", map[string]interface{}{ "output": "hostid", "filter": map[string]interface{}{ "status": 0 } })
	enabledHosts := []interface{}{}
	if err == nil { enabledHosts = enabledResp["result"].([]interface{}) }
	nEnabledHosts := len(enabledHosts)

	disabledResp, err := zabbixApiRequest(apiUrl, token, "host.get", map[string]interface{}{ "output": "hostid", "filter": map[string]interface{}{ "status": 1 } })
	disabledHosts := []interface{}{}
	if err == nil { disabledHosts = disabledResp["result"].([]interface{}) }
	nDisabledHosts := len(disabledHosts)

	// 3.1 Templates (contagem)
	if progressCb != nil { progressCb("Coletando informações de Templates...") }
	templatesCount := "N/A"
	templatesCountResp, err := zabbixApiRequest(apiUrl, token, "template.get", map[string]interface{}{
		"countOutput": true,
	})
	if err == nil {
		templatesCount = fmt.Sprintf("%v", templatesCountResp["result"])
	}

	// 3.2 Itens (total / habilitados / desabilitados / não suportados)
	if progressCb != nil { progressCb("Coletando informações de Itens (contagens)...") }
	nItemsTotal := "-"
	nItemsEnabled := "-"
	nItemsDisabled := "-"
	// total de itens (usar params estendidos: output extend, countOutput, templated false, webitems true)
	itemsTotalResp, err := zabbixApiRequest(apiUrl, token, "item.get", map[string]interface{}{
		"output": "extend",
		"countOutput": true,
		"templated": false,
		"webitems": true,
	})
	if err == nil {
		nItemsTotal = fmt.Sprintf("%v", itemsTotalResp["result"])
	}
	// itens habilitados (usar params mais completos: monitored, templated, webitems, filter status/state)
	itemsEnabledResp, err := zabbixApiRequest(apiUrl, token, "item.get", map[string]interface{}{
		"countOutput": true,
		"monitored": true,
		"templated": false,
		"webitems": true,
		"filter": map[string]interface{}{ "status": 0, "state": 0 },
	})
	if err == nil {
		nItemsEnabled = fmt.Sprintf("%v", itemsEnabledResp["result"])
	}
	// itens desabilitados
	itemsDisabledResp, err := zabbixApiRequest(apiUrl, token, "item.get", map[string]interface{}{
		"countOutput": true,
		"filter": map[string]interface{}{ "status": 1 },
	})
	if err == nil {
		nItemsDisabled = fmt.Sprintf("%v", itemsDisabledResp["result"])
	}

	// 4. Itens não suportados
	if progressCb != nil { progressCb("Coletando itens não suportados...") }
	itemsResp, err := zabbixApiRequest(apiUrl, token, "item.get", map[string]interface{}{
		"output": []string{"itemid","name","templateid","error","key_"},
		"filter": map[string]interface{}{ "state": 1 },
		"webitems": 1,
		"selectHosts": []string{"name","hostid"},
		"inherited": true,
	})
	items := []interface{}{}
	if err == nil { items = itemsResp["result"].([]interface{}) }

	// Buscar nome do template real de cada item (usar templateid como chave, conforme script Python)
	// Primeiro crio a lista única de templateids (fake) retornados nos itens não suportados
	templateFakeSet := map[string]struct{}{}
	for _, i := range items {
		item := i.(map[string]interface{})
		tplId := fmt.Sprintf("%v", item["templateid"])
		if tplId == "0" { continue }
		templateFakeSet[tplId] = struct{}{}
	}
	templateFakeIds := []string{}
	for id := range templateFakeSet { templateFakeIds = append(templateFakeIds, id) }

	// cacheTemplateItems maps templateid (fake) -> template host name (o nome do template)
	cacheTemplateItems := map[string]string{}
	cacheTemplateHostID := map[string]string{}
	if len(templateFakeIds) > 0 {
		cacheResp, err := zabbixApiRequest(apiUrl, token, "item.get", map[string]interface{}{
			"output": []string{"name", "key_", "templateid"},
			"itemids": templateFakeIds,
			"selectHosts": []string{"name", "hostid"},
		})
		if err == nil {
			for _, item := range cacheResp["result"].([]interface{}) {
				itemMap := item.(map[string]interface{})
				itemid := fmt.Sprintf("%v", itemMap["itemid"]) 
				hostsArr, _ := itemMap["hosts"].([]interface{})
				templateName := ""
				templateHostId := ""
				if len(hostsArr) > 0 {
					host := hostsArr[0].(map[string]interface{})
					templateName = fmt.Sprintf("%v", host["name"])
					templateHostId = fmt.Sprintf("%v", host["hostid"]) 
				}
				cacheTemplateItems[itemid] = templateName
				cacheTemplateHostID[itemid] = templateHostId
			}
		}
	}

	// Agrupamentos
	templateCounter := map[string]int{}
	hostCounter := map[string]int{}
	itemCounter := map[string]int{}
	errorCounter := map[string]int{}
	templateItems := map[string][][]string{}
	hostItems := map[string][][]string{}
	for _, i := range items {
		item := i.(map[string]interface{})
		tplId := fmt.Sprintf("%v", item["templateid"])
		if tplId == "0" { continue }
		itemId := fmt.Sprintf("%v", item["itemid"])
		itemName := fmt.Sprintf("%v", item["name"])
		itemError := fmt.Sprintf("%v", item["error"])
		hostsArr := item["hosts"].([]interface{})
		itemHostName := ""
		itemHostId := ""
		if len(hostsArr) > 0 {
			host := hostsArr[0].(map[string]interface{})
			itemHostName = fmt.Sprintf("%v", host["name"])
			itemHostId = fmt.Sprintf("%v", host["hostid"])
		}
		urlEdit := url + "/items.php?form=update&hostid=" + itemHostId + "&itemid=" + itemId + "&context=host"
		// usar templateid (tplId) como chave e recuperar o nome do cache
		templateName := cacheTemplateItems[tplId]
		if templateName == "" {
			// fallback: se cache não tiver, tentar buscar pelo itemid (compatibilidade)
			templateName = cacheTemplateItems[itemId]
		}
		templateCounter[tplId]++
		hostCounter[itemHostName]++
		itemCounter[itemName+"|"+tplId]++
		errorCounter[itemError+"|"+tplId]++
		templateItems[tplId] = append(templateItems[tplId], []string{itemName, itemError, itemHostName, urlEdit, templateName})
		hostItems[itemHostName] = append(hostItems[itemHostName], []string{itemName, itemError, tplId, urlEdit})
	}

	// Buscar nomes dos templates
	templateNames := map[string]string{}
	if len(templateCounter) > 0 {
		tplIds := []string{}
		for tplId := range templateCounter { tplIds = append(tplIds, tplId) }
		tplResp, err := zabbixApiRequest(apiUrl, token, "template.get", map[string]interface{}{
			"output": []string{"templateid","name"},
			"templateids": tplIds,
		})
		if err == nil {
			for _, tpl := range tplResp["result"].([]interface{}) {
				tplMap := tpl.(map[string]interface{})
				templateNames[fmt.Sprintf("%v", tplMap["templateid"])] = fmt.Sprintf("%v", tplMap["name"])
			}
		}
		// preencha nomes a partir do cache também (fallback/compatibilidade)
		for id, name := range cacheTemplateItems { if templateNames[id] == "" { templateNames[id] = name } }
	}

	// Top N
	topN := 10
	type kv struct{ Key string; Value int }
	sortMap := func(m map[string]int) []kv {
		arr := []kv{}
		for k, v := range m { arr = append(arr, kv{k, v}) }
		sort.Slice(arr, func(i, j int) bool { return arr[i].Value > arr[j].Value })
		return arr
	}
	topTemplates := sortMap(templateCounter)
	if len(topTemplates) > topN { topTemplates = topTemplates[:topN] }
	topHosts := sortMap(hostCounter)
	if len(topHosts) > topN { topHosts = topHosts[:topN] }
	topItems := sortMap(itemCounter)
	if len(topItems) > topN { topItems = topItems[:topN] }
	topErrors := sortMap(errorCounter)
	if len(topErrors) > topN { topErrors = topErrors[:topN] }

	// Descrições (textos exibidos nas view-boxes)
	descTemplates := "Revise os templates com maior número de itens problemáticos. Verifique se estão atualizados, compatíveis com a versão do Zabbix e se os itens monitorados ainda fazem sentido para o ambiente. Considere simplificar ou dividir templates muito grandes."
	descHosts := "Analise os hosts com mais erros. Verifique conectividade, permissões, agentes instalados e se o host está ativo. Corrija configurações específicas ou remova hosts obsoletos do monitoramento."
	descItens := "Itens recorrentes podem indicar falhas de configuração, incompatibilidade ou ausência de recursos no host. Revise a chave do item, parâmetros, dependências externas (scripts, drivers, diretórios) e ajuste o template conforme necessário."
	descErros := "Para cada tipo de erro, consulte a documentação do Zabbix e do sistema operacional/serviço monitorado. Corrija chaves inválidas, permissões, dependências, drivers ou scripts ausentes. Ajuste preprocessamento e tipos de dados conforme o erro apresentado. Para itens SNMP, verifique se o firmware do equipamento está atualizado, valide o OID utilizado e confirme se a comunidade SNMP está correta e configurada no dispositivo."
	descDetalhamento := "Analise cada item e erro detalhado. Acesse o link para editar o item diretamente no Zabbix, revise a configuração, ajuste parâmetros e valide se o item é realmente necessário."
	descNaoSuportados := "Os itens não suportados são aqueles que estão ativos, porém no momento de efetuar a coleta houve um erro. Esses itens continuam consumindo os processos do Zabbix desnecessariamente. Clique no link e analise caso a caso para correção. Itens que não são mais necessários devem ser removidos para otimizar o desempenho do Zabbix."

	// --- HTML moderno ---
	html := `<div class='zabbix-report-modern'>`
		// Global tooltip CSS/JS (single copy) - info-icon + info-tooltip
		html += `<style>
		.info-icon{display:inline-flex;align-items:center;justify-content:center;width:18px;height:18px;cursor:pointer;margin-left:6px;position:relative}
		.info-icon svg{display:block}
		.info-tooltip{display:none;position:absolute;z-index:40;left:22px;top:50%;transform:translateY(-50%);background:#e3f2fd;color:#102a43;padding:8px 12px;border-radius:8px;box-shadow:0 6px 24px rgba(0,0,0,0.12);font-size:13px;min-width:360px;max-width:auto;white-space:normal;word-break:normal;overflow-wrap:break-word}
		.info-icon:focus .info-tooltip, .info-icon:hover .info-tooltip{display:block}
		</style>
		<script>
		function setupInfoTooltips(){
			document.querySelectorAll('.info-icon').forEach(function(icon){
				if(icon._tooltipBound) return; icon._tooltipBound = true;
				icon.addEventListener('click', function(e){
					var tip = this.querySelector('.info-tooltip');
					if(tip){ tip.style.display = (tip.style.display==='block') ? 'none' : 'block'; }
					e.stopPropagation();
				});
			});
		}
		document.addEventListener('click', function(){ document.querySelectorAll('.info-tooltip').forEach(function(t){ t.style.display='none'; }); });
		// call once now and again after dynamic inserts
		setTimeout(setupInfoTooltips, 10);
		</script>`

		// helper to render headings with info icon and tooltip
		titleWithInfo := func(tag, title, tip string) string {
				// build SVG question-circle and tooltip content
				sv := `<svg viewBox='0 0 16 16' width='14' height='14' aria-hidden='true'><circle cx='8' cy='8' r='7' stroke='#1976d2' stroke-width='1.6' fill='white'/><text x='8' y='11' text-anchor='middle' font-size='10' fill='#1976d2' font-family='Arial' font-weight='bold'>?</text></svg>`
			tipEsc := htmlpkg.EscapeString(tip) // Escape the tooltip text for HTML
				// place tooltip inside the icon span so positioning is relative
				return fmt.Sprintf("<%s>%s <span class='info-icon' tabindex='0'>%s<span class='info-tooltip'>%s</span></span></%s>", tag, htmlpkg.EscapeString(title), sv, tipEsc, tag)
		}
	// compute ambiente and version for header
	ambienteUrl := url
	if strings.HasSuffix(ambienteUrl, "/api_jsonrpc.php") {
		ambienteUrl = ambienteUrl[:len(ambienteUrl)-len("/api_jsonrpc.php")]
	}
	verLabel := "N/A"
	if zabbixVersion != "" { verLabel = zabbixVersion }

	// Tabs UI (UX: simpler navigation, grouped content)
	html += `<style>` +
		`.tabs-container{display:flex;gap:8px;flex-wrap:nowrap;overflow-x:auto;margin-top:12px;margin-bottom:14px;}` +
		`.tab-btn{padding:10px 14px;border-radius:6px;border:1px solid #d1d5db;background:#ffffff;color:#102a43;font-weight:600;cursor:pointer;white-space:nowrap;}` +
		`.tab-btn.active{background:#0b69d6;color:#fff;border-color:#0b69d6}` +
		`.tab-panel{padding-top:12px;}` +
	`</style>`
	html += `<div style='display:flex;align-items:center;justify-content:space-between;gap:12px;'>`
	html += `<div style='font-size:14px;color:#1f2937;'><strong>Ambiente:</strong> ` + htmlpkg.EscapeString(ambienteUrl) + `</div>`
	html += `<div style='font-size:14px;color:#1f2937;'><strong>Versão:</strong> ` + htmlpkg.EscapeString(verLabel) + `</div>`
	html += `</div>`
	html += `<div class='tabs-container'>`
	html += `<button class='tab-btn active' data-tab='tab-resumo'>Resumo do Ambiente</button>`
	html += `<button class='tab-btn' data-tab='tab-processos'>Zabbix Server</button>`
	html += `<button class='tab-btn' data-tab='tab-proxys'>Zabbix Proxys</button>`
	html += `<button class='tab-btn' data-tab='tab-top'>Top Templates/Itens</button>`
	html += `<button class='tab-btn' data-tab='tab-items'>Items e LLDs</button>`
	html += `<button class='tab-btn' data-tab='tab-templates'>Templates</button>`
	html += `<button class='tab-btn' data-tab='tab-recomendacoes'>Recomendações</button>`
	html += `</div>`

	// Tab panels: resumo (visible), others hidden by default
	html += `<div id='tab-resumo' class='tab-panel' style='display:block;'>`
	html += `<div class='table-responsive'><table class='modern-table'><thead><tr><th>Parâmetro</th><th>Valor</th><th>Detalhes</th></tr></thead><tbody>`
	// Hosts
	html += `<tr><td>Número de hosts (habilitados/desabilitados)</td><td>` + fmt.Sprintf("%d", nTotalHosts) + `</td><td>` + fmt.Sprintf("%d / %d", nEnabledHosts, nDisabledHosts) + `</td></tr>`
	// Templates
	html += `<tr><td>Número de templates</td><td>` + templatesCount + `</td><td></td></tr>`
	// Itens
	html += `<tr><td>Número de itens (habilitados/desabilitados/não suportados)</td><td>` + nItemsTotal + `</td><td>` + nItemsEnabled + ` / ` + nItemsDisabled + ` / ` + nItensNaoSuportados + `</td></tr>`
	// Proxys
	if progressCb != nil { progressCb("Coletando informações de Proxys...") }
	proxyCount := 0
	var proxies []map[string]interface{}
	if pc, perr := getProxyCount(apiUrl, token); perr == nil { proxyCount = pc } else { log.Printf("[ERROR] proxy.get failed: %v", perr) }
	// fetch full proxy list to render names and types
	if plist, perr2 := getProxies(apiUrl, token); perr2 == nil && plist != nil { proxies = plist } else if perr2 != nil { log.Printf("[ERROR] proxy.get (list) failed: %v", perr2) }
	html += `<tr><td>Número de Proxys</td><td>` + fmt.Sprintf("%d", proxyCount) + `</td><td></td></tr>`
	// Usuários
	html += `<tr><td>Número de usuários</td><td>` + fmt.Sprintf("%d", nUsers) + `</td><td></td></tr>`
	// NVPS
	html += `<tr><td>Required server performance, new values per second</td><td>` + nvps + `</td><td></td></tr>`
	html += `</tbody></table></div>`

	// descriptions for tooltip (lowercase key) - moved here so pollers section can reference it
	procDesc := map[string]string{
		"agent poller": `Parâmetro "StartPollers": especifica o número de processadores de coleta (pollers) para itens passivos. Aumente este valor para elevar a taxa de processamento de verificações passivas.`,
		"alert manager": `Parâmetro "AlertManager": gerencia a fila de alertas e coordena o encaminhamento para o serviço de envio de notificações. Ajuste quando houver atraso no processamento de alertas.`,
		"alert syncer": `Parâmetro "AlertSyncer": responsável por gravar alertas na base de dados; aumente se há acúmulo de gravações.`,
		"alerter": `Parâmetro "Alerter": processo responsável pelo envio efetivo de notificações (e-mail, SMS, etc.); aumente para melhorar paralelismo no envio.`,
		"availability manager": `Parâmetro "AvailabilityManager": atualiza o estado de disponibilidade de hosts; relevante quando há muitas mudanças de status.`,
		"browser poller": `Parâmetro "StartBrowserPollers": poller responsável por verificações de navegador (web scenarios). Aumente para mais paralelismo em testes de páginas web.`,
		"configuration syncer": `Parâmetro "ConfigurationSyncer": sincroniza a configuração em memória do servidor Zabbix; aumente se há demora na aplicação de alterações de configuração.`,
		"configuration syncer worker": `Parâmetro "ConfigurationSyncerWorker": worker do Configuration Syncer que resolve macros de usuário e sincroniza nomes/valores; ajuste quando houver alto volume de mudanças de macros.`,
		"connector manager": `Parâmetro "ConnectorManager": gerencia conectores externos e enfileiramento de requisições de integração com serviços terceiros; dimensione conforme integrações ativas.`,
		"connector worker": `Parâmetro "ConnectorWorker": processos trabalhadores do conector que executam requisições e tratam respostas de sistemas externos; aumente se integrações ficarem lentas.`,
		"discovery manager": `Parâmetro "StartDiscoverers": gerencia tarefas de descoberta de hosts/serviços em redes; aumente para acelerar varreduras em grandes ambientes.`,
		"discovery worker": `Parâmetro "DiscoveryWorker": trabalhadores de descoberta que executam verificações (SNMP, ICMP, etc.) solicitadas pelo discovery manager; escale conforme necessidade de varreduras.`,
		"escalator": `Parâmetro "Escalator": processo de escalonamento de ações automatizadas (ex.: escalonamento de alertas); relevante em regras de escalonamento complexas.`,
		"ha manager": `Parâmetro "HAManager": gerencia recursos de alta disponibilidade entre servidores Zabbix em cluster; ajuste quando usar HA e notar latência na sincronização.`,
		"history poller": `Parâmetro "StartPollers": pollers que coletam dados calculados ou requerem conexão com o banco; aumente se cálculo de itens estiver atrasado.`,
		"history syncer": `Parâmetro "HistorySyncer": escritor de histórico que persiste dados de itens no banco de dados; dimensione para reduzir gargalos de gravação.`,
		"housekeeper": `Parâmetro "HousekeeperFrequency": remove dados históricos antigos e limpa tabelas; aumente se limpeza estiver lenta e o banco ficar grande. Possui total relação com o Banco de Dados, se é particionado ou não.`,
		"http agent poller": `Parâmetro "StartHTTPPollers": poller assíncrono para checagens HTTP com threads de trabalho; aumente para maior concorrência em sondagens web.`,
		"http poller": `Parâmetro "StartHTTPPollers": poller para monitoramento HTTP síncrono; ajuste conforme volume de web checks.`,
		"icmp pinger": `Parâmetro "StartPingers": pollers de ICMP (ping) para verificar disponibilidade de hosts; aumente se muitos hosts necessitam de verificação frequente.`,
		"internal poller": `Parâmetro "InternalPollers": poller para verificações internas do servidor Zabbix (saúde e métricas internas); ajuste se coleta interna estiver sobrecarregada.`,
		"ipmi manager": `Parâmetro "IPMIManager": gerencia pollers IPMI para coletar métricas via IPMI; dimensione quando houver muitos dispositivos IPMI.`,
		"ipmi poller": `Parâmetro "IPMIPollers": poller que executa checagens IPMI em equipamentos; aumente para maior paralelismo em ambientes com IPMI extensivo.`,
		"java poller": `Parâmetro "JavaPollers": poller para coletas do tipo Java; ajuste quando houver muitos checadores baseados em Java.`,
		"lld manager": `Parâmetro "LLDManager": gerencia tarefas de Low-Level Discovery (LLD) que detectam automaticamente serviços/instâncias; aumente para acelerar descobertas em larga escala.`,
		"lld worker": `Parâmetro "LLDWorkers": workers do LLD que executam regras de descoberta e geram itens/dispositivos; dimensione conforme a frequência e quantidade de regras LLD.`,
		"odbc poller": `Parâmetro "ODBCPollers": poller responsável por consultas ODBC/DB para coleta de métricas; aumente se houver muitos checks dependentes de ODBC.`,
		"poller": `Parâmetro "StartPollers": processadores de coleta padrão para itens passivos; afeta throughput geral de coleta.`,
		"preprocessing manager": `Parâmetro "PreprocessingManager": gerencia tarefas de pré-processamento de dados (regexp, transform) antes de serem salvo em banco de dados; aumente para reduzir filas de processamento.`,
		"preprocessing worker": `Parâmetro "PreprocessingWorkers": threads que executam transformações/preprocessamento de valores antes de armazenamento ou avaliação de triggers.`,
		"proxy poller": `Parâmetro "ProxyPollers": pollers para proxies remotos que agregam dados de agentes/proxies; ajuste para balancear carga de proxies.`,
		"proxy group manager": `Parâmetro "ProxyGroupManager": gerencia balanceamento e alta disponibilidade entre proxies; dimensione se usar múltiplos proxies para redundância.`,
		"report manager": `Parâmetro "ReportManager": gerencia tarefas agendadas de geração de relatórios; aumente se houver acúmulo na geração de relatórios.`,
		"report writer": `Parâmetro "ReportWriter": processos responsáveis por gerar e gravar relatórios programados; aumente quando a produção de relatórios estiver atrasada.`,
		"self-monitoring": `Parâmetro "SelfMonitoring": coleta métricas internas do servidor (uso CPU, filas internas); importante para avaliação de saúde do Zabbix Server.`,
		"service manager": `Parâmetro "ServiceManager": gerencia eventos de serviço, tags e recuperação de problemas vindo de vários componentes; escale se processamento de eventos estiver lento.`,
		"snmp poller": `Parâmetro "StartSNMPPollers": poller assíncrono para checagens SNMP (walk/get) com threads; aumente para maior concorrência em ambientes SNMP extensos.`,
		"snmp trapper": `Parâmetro "SNMPTrapper": componente que recebe e processa traps SNMP; aumente para maior taxa de ingestão de traps.`,
		"task manager": `Parâmetro "TaskManager": executa tarefas remotas solicitadas por outros componentes (fechar problema, executar comando remoto, etc.); aumente para reduzir latência de execução de tarefas.`,
		"timer": `Parâmetro "Timer": responsável pelo agendamento de tarefas periódicas e manutenção; ajuste se agendamentos estiverem atrasados.`,
		"trapper": `Parâmetro "StartTrappers": processo que recebe itens ativos, traps e comunicação de proxies; dimensione para aumentar ingestão de dados ativos.`,
		"trigger housekeeper": `Parâmetro "TriggerHousekeeper": remove problemas/triggers órfãos ou deletados; aumente se houver acúmulo de entradas a limpar.`,
		"unreachable poller": `Parâmetro "StartPollersUnreachable": poller específico para hosts considerados inatingíveis; aumente para acelerar rechecagens desses hosts.`,
		"vmware collector": `Parâmetro "StartVMwareCollectors": coletor para integrações VMware responsável por consultar APIs VMware; aumente para maior paralelismo em ambientes virtualizados grandes.`,
	}

	// Prepare numeric totals for gauge
	totalItemsVal := 0
	unsupportedVal := 0
	// itemsNoTplCount será preenchido posteriormente (Items sem Template)
	itemsNoTplCount := 0
	if nItemsTotal != "-" {
		if v, err := strconv.Atoi(strings.TrimSpace(nItemsTotal)); err == nil { totalItemsVal = v }
	}
	if nItensNaoSuportados != "-" {
		if v, err := strconv.Atoi(strings.TrimSpace(nItensNaoSuportados)); err == nil { unsupportedVal = v }
	}
	if totalItemsVal < 0 { totalItemsVal = 0 }
	if unsupportedVal < 0 { unsupportedVal = 0 }
	supportedVal := totalItemsVal - unsupportedVal
	if supportedVal < 0 { supportedVal = 0 }

	// Gauge area (pie/doughnut) - reserve space for multiple gauges later
	html += `<div class='summary-gauges' style='display:flex;gap:18px;flex-wrap:wrap;margin-top:12px;align-items:flex-start;'>`
	// Hosts gauge (left)
	html += `<div class='card' style='background:#fff;color:#222;padding:12px;border-radius:8px;min-width:220px;box-shadow:0 1px 6px rgba(0,0,0,0.04);'>`
	html += `<h4 style='margin:0 0 8px 0;'>Hosts: Desabilitados</h4>`
	html += `<canvas id='hosts-gauge' width='200' height='200' style='max-width:200px;' data-total='` + fmt.Sprintf("%d", nTotalHosts) + `' data-unsupported='` + fmt.Sprintf("%d", nDisabledHosts) + `' data-unsupported-label='Desabilitados' data-supported-label='Habilitados' data-color-unsupported='#ffcc66' data-color-supported='#66c2a5'></canvas>`
	// legend lines: color swatches and separated lines (supported / disabled)
	hostDisabledPct := 0
	if nTotalHosts > 0 { hostDisabledPct = (nDisabledHosts * 100) / nTotalHosts }
	html += `<div class='gauge-legend' style='width:100%;margin-top:8px;font-size:0.95em;'>`
	html += `<div style='display:flex;align-items:center;gap:8px;margin-bottom:6px;'><span style='display:inline-block;width:12px;height:12px;background:#66c2a5;border-radius:3px;'></span><strong>Total hosts:</strong>&nbsp;` + fmt.Sprintf("%d", nTotalHosts) + `</div>`
	html += `<div style='display:flex;align-items:center;gap:8px;'><span style='display:inline-block;width:12px;height:12px;background:#ffcc66;border-radius:3px;'></span><strong>Desabilitados:</strong>&nbsp;` + fmt.Sprintf("%d", nDisabledHosts) + ` (` + fmt.Sprintf("%d", hostDisabledPct) + `%)</div>`
	html += `</div>`
	html += `</div>`
	// Items gauge (right)
	html += `<div class='card' style='background:#fff;color:#222;padding:12px;border-radius:8px;min-width:220px;box-shadow:0 1px 6px rgba(0,0,0,0.04);'>`
	html += `<h4 style='margin:0 0 8px 0;'>Itens: Não Suportados</h4>`
	html += `<canvas id='items-gauge' width='200' height='200' style='max-width:200px;' data-total='` + fmt.Sprintf("%d", totalItemsVal) + `' data-unsupported='` + fmt.Sprintf("%d", unsupportedVal) + `' data-unsupported-label='Não suportados' data-supported-label='Suportados' data-color-unsupported='#ff7a7a' data-color-supported='#66c2a5'></canvas>`
	// legend lines for items
	itemsUnsupportedPct := 0
	if totalItemsVal > 0 { itemsUnsupportedPct = (unsupportedVal * 100) / totalItemsVal }
	html += `<div class='gauge-legend' style='width:100%;margin-top:8px;font-size:0.95em;'>`
	html += `<div style='display:flex;align-items:center;gap:8px;margin-bottom:6px;'><span style='display:inline-block;width:12px;height:12px;background:#66c2a5;border-radius:3px;'></span><strong>Total items:</strong>&nbsp;` + fmt.Sprintf("%d", totalItemsVal) + `</div>`
	html += `<div style='display:flex;align-items:center;gap:8px;'><span style='display:inline-block;width:12px;height:12px;background:#ff7a7a;border-radius:3px;'></span><strong>Não suportados:</strong>&nbsp;` + fmt.Sprintf("%d", unsupportedVal) + ` (` + fmt.Sprintf("%d", itemsUnsupportedPct) + `%)</div>`
	html += `</div>`
	html += `</div>`
	html += `</div>`


	// close Resumo tab and open Top Templates tab
	html += `</div>` // end tab-resumo

	// --- Top Templates/Itens tab ---
	html += `<div id='tab-top' class='tab-panel' style='display:none;'>`
	// Top Templates Ofensores
	html += titleWithInfo("h3", "Top Templates Ofensores", "Como corrigir: " + descTemplates)
	html += `<div class='table-responsive'><table class='modern-table'><thead><tr><th>Template</th><th>Quantidade de Erros</th></tr></thead><tbody>`
	for _, tpl := range topTemplates {
		tplName := templateNames[tpl.Key]
		if tplName == "" { tplName = tpl.Key }
		html += `<tr><td>` + tplName + `</td><td>` + fmt.Sprintf("%d", tpl.Value) + `</td></tr>`
	}
	html += `</tbody></table></div>`

	// Top Hosts Ofensores (com template mais recorrente)
	html += titleWithInfo("h3", "Top Hosts Ofensores", "Como corrigir: " + descHosts)
	html += `<div class='table-responsive'><table class='modern-table'><thead><tr><th>Host</th><th>Template Mais Ofensor</th><th>Quantidade de Erros</th></tr></thead><tbody>`
	for _, host := range topHosts {
		// Descobrir o template mais recorrente para o host
		tplCount := map[string]int{}
		for _, row := range hostItems[host.Key] {
			tplCount[row[2]]++
		}
		mainTplId := ""
		maxCount := 0
		for tplId, count := range tplCount {
			if count > maxCount { mainTplId = tplId; maxCount = count }
		}
		mainTplName := templateNames[mainTplId]
		if mainTplName == "" { mainTplName = mainTplId }
		html += `<tr><td>` + host.Key + `</td><td>` + mainTplName + `</td><td>` + fmt.Sprintf("%d", host.Value) + `</td></tr>`
	}
	html += `</tbody></table></div>`

	// Top Itens Problemáticos
	html += titleWithInfo("h3", "Top Itens Problemáticos", "Como corrigir: " + descItens)
	html += `<div class='table-responsive'><table class='modern-table'><thead><tr><th>Item</th><th>Template</th><th>Quantidade de Erros</th></tr></thead><tbody>`
	for _, item := range topItems {
		parts := strings.SplitN(item.Key, "|", 2)
		itemName := parts[0]
		tplId := ""
		if len(parts) > 1 { tplId = parts[1] }
		tplName := templateNames[tplId]
		if tplName == "" { tplName = tplId }
		html += `<tr><td>` + itemName + `</td><td>` + tplName + `</td><td>` + fmt.Sprintf("%d", item.Value) + `</td></tr>`
	}
	html += `</tbody></table></div>`

	// Tipos de Erro Mais Comuns
	html += titleWithInfo("h3", "Tipos de Erro Mais Comuns", "Como corrigir: " + descErros)
	html += `<div class='table-responsive'><table class='modern-table'><thead><tr><th>Mensagem de Erro</th><th>Template</th><th>Ocorrências</th></tr></thead><tbody>`
	for _, errRow := range topErrors {
		parts := strings.SplitN(errRow.Key, "|", 2)
		errMsg := parts[0]
		tplId := ""
		if len(parts) > 1 { tplId = parts[1] }
		tplName := templateNames[tplId]
		if tplName == "" { tplName = tplId }
		html += `<tr><td>` + errMsg + `</td><td>` + tplName + `</td><td>` + fmt.Sprintf("%d", errRow.Value) + `</td></tr>`
	}
	html += `</tbody></table></div>`

	// close Top tab after Top Errors and open Processos tab will be inserted later
	html += `</div>` // end tab-top

	       // --- Processos e Threads Zabbix Server (Pollers + Internal) ---
		if progressCb != nil { progressCb("Coletando informações de Pollers e Processos internos...") }
		// Get CHECKTRENDTIME as string for display (default "30")
	       checkTrendStr := os.Getenv("CHECKTRENDTIME")
	       if checkTrendStr == "" { checkTrendStr = "30d" }
	       // Extract numeric part (days/hours/minutes)
	       checkTrendDisplay := "30 dias"
	       if len(checkTrendStr) > 1 {
		       unit := checkTrendStr[len(checkTrendStr)-1]
		       numPart := checkTrendStr[:len(checkTrendStr)-1]
		       if unit == 'd' {
			       checkTrendDisplay = numPart + " dias"
		       } else if unit == 'h' {
			       checkTrendDisplay = numPart + " horas"
		       } else if unit == 'm' {
			       checkTrendDisplay = numPart + " minutos"
		       } else {
			       checkTrendDisplay = checkTrendStr + " (minutos)"
		       }
	       }
	       html += `<div id='tab-processos' class='tab-panel' style='display:none;'>`
	       serverHost := os.Getenv("ZABBIX_SERVER_HOSTID")
	       if serverHost == "" {
		       log.Printf("[DEBUG] ZABBIX_SERVER_HOSTID not set; searching without hostid for pollers")
	       } else {
		       log.Printf("[DEBUG] ZABBIX_SERVER_HOSTID=%s will be used for pollers", serverHost)
	       }
	       // build poller list conditionally based on Zabbix major version
	       pollerNames := []string{}
	       // pollers available in both 6 and 7
	       commonPollers := []string{
		       "poller",
		       "http poller",
		       "icmp pinger",
		       "ipmi poller",
		       "java poller",
		       "odbc poller",
		       "proxy poller",
		       "unreachable poller",
		       "preprocessing worker",
		       "preprocessing manager",
		       "vmware collector",
	       }
	       pollerNames = append(pollerNames, commonPollers...)
	       // pollers introduced / better represented in Zabbix 7
	       if majorV >= 7 {
		       pollerNames = append([]string{`agent poller`, `browser poller`, `http agent poller`, `snmp poller`}, pollerNames...)
	       } else {
		       // for Zabbix 6, include SNMP trapper as separate component if desired (kept out of pollers)
	       }
		html += titleWithInfo("h3", "Pollers (Data Collectors)", `Os pollers (de forma passiva) consultam ativamente os agentes configurados, em intervalos definidos para coletar as métricas. Isso contrasta com o modo passivo (trappers), onde os agentes enviam dados automaticamente ao servidor; porém eles também podem ser sobrecarregados quando há aumento de fila. Para otimizar, aumente gradualmente o número de Pollers no arquivo zabbix_server.conf quando houver degradação. As decisões de ajuste devem basear-se nas tendências dos últimos ` + checkTrendDisplay + `: se a utilização média estiver consistentemente entre 50% e 60% e os picos ultrapassarem 60%, considere aumentar os pollers; se estiver abaixo de 50%, normalmente não há necessidade de aumento.`)		
	html += `<div class='table-responsive'><table class='modern-table'><thead><tr><th>Poller</th><th>value_min</th><th>value_avg</th><th>value_max</th><th>Status</th></tr></thead><tbody>`
	type pollRow struct{
		Friendly string
		Desc string
		Smin string
		Savg string
		Smax string
		Vmax float64
		Vavg float64
		StatusText string
		StatusStyle string
		Disabled bool
		DisabledMsg string
		Err bool
	}
	pollRows := []pollRow{}
	for _, name := range pollerNames {
		baseName := strings.ToLower(strings.TrimSpace(name))
		desc := procDesc[baseName]
		if desc == "" { desc = "Poller process" }
		words := strings.Fields(name)
		for i, w := range words { words[i] = strings.Title(strings.TrimSpace(w)) }
		friendly := strings.Join(words, " ")

		pr := pollRow{Friendly: friendly, Desc: desc, Disabled: false, Err: false, Vmax: -1}

		key := fmt.Sprintf("zabbix[process,%s,avg,busy]", name)
		item, ierr := getItemByKey(apiUrl, token, key, serverHost)
		if ierr != nil {
			pr.Err = true
			pollRows = append(pollRows, pr)
			continue
		}
		if item == nil {
			pr.Disabled = true
			if serverHost != "" {
				// verify whether the host id actually exists in Zabbix
				hostExists := false
				hostParams := map[string]interface{}{"output": []string{"hostid"}, "hostids": []string{serverHost}}
				if hresp, herr := zabbixApiRequest(apiUrl, token, "host.get", hostParams); herr == nil {
					if rr, ok := hresp["result"]; ok {
						if arr, ok2 := rr.([]interface{}); ok2 && len(arr) > 0 {
							hostExists = true
						}
					}
				}
				if !hostExists {
					pr.DisabledMsg = fmt.Sprintf("Hostid %s não encontrado, informe o valor na ENV ZABBIX_SERVER_HOSTID.", serverHost)
				} else {
					// host exists but item not found: either not enabled or not present in this Zabbix version
					if majorV < 7 {
						// version-specific pollers that may not exist in older versions
						switch strings.ToLower(strings.TrimSpace(name)) {
						case "agent poller", "browser poller", "http agent poller", "snmp poller":
							pr.DisabledMsg = "Não existe nesta versão do Zabbix"
						default:
							pr.DisabledMsg = "Processo não habilitado"
						}
					} else {
						pr.DisabledMsg = "Processo não habilitado"
					}
				}
			} else {
				// no serverHost provided: decide based on version/known names
				if majorV < 7 {
					switch strings.ToLower(strings.TrimSpace(name)) {
					case "agent poller", "browser poller", "http agent poller", "snmp poller":
						pr.DisabledMsg = "Não existe nesta versão do Zabbix"
					default:
						pr.DisabledMsg = "Processo não habilitado"
					}
				} else {
					pr.DisabledMsg = "Processo não habilitado"
				}
			}
			pollRows = append(pollRows, pr)
			continue
		}
		itemid := fmt.Sprintf("%v", item["itemid"])
		trend, terr := getLastTrend(apiUrl, token, itemid, 30)
		if terr != nil {
			pr.Err = true
			pollRows = append(pollRows, pr)
			continue
		}
		if trend == nil {
			pr.Disabled = true
			// item exists but no trend data -> likely not enabled
			pr.DisabledMsg = "Processo não habilitado"
			pollRows = append(pollRows, pr)
			continue
		}
		parseVal := func(k string) float64 {
			if v, ok := trend[k]; ok {
				s := fmt.Sprintf("%v", v)
				if f, err := strconv.ParseFloat(s, 64); err == nil { return f }
			}
			return -1
		}
		vmin := parseVal("value_min")
		vavg := parseVal("value_avg")
		vmax := parseVal("value_max")
		pr.Vmax = vmax
		pr.Vavg = vavg
		fmtVal := func(f float64) string {
			if f < 0 { return "-" }
			return fmt.Sprintf("%.2f%%", f)
		}
		pr.Smin = fmtVal(vmin)
		pr.Savg = fmtVal(vavg)
		pr.Smax = fmtVal(vmax)
		if vavg >= 0 {
			// Two-state logic: OK if vavg < 49.9%, otherwise Atenção
			if vavg < 49.9 {
				pr.StatusText = "OK"
				pr.StatusStyle = "background:#66c28a;color:#000;padding:6px;border-radius:4px;text-align:center;"
			} else {
				pr.StatusText = "Atenção"
				pr.StatusStyle = "background:#ff6666;color:#000;padding:6px;border-radius:4px;text-align:center;"
			}
		} else {
			pr.StatusText = "-"
		}
		pollRows = append(pollRows, pr)
	}
	// sort by Vavg desc (items with -1 go last)
	sort.Slice(pollRows, func(i, j int) bool {
		return pollRows[i].Vavg > pollRows[j].Vavg
	})
	// render
	       for _, pr := range pollRows {
		       nameCell := `<td style='position:relative;padding:0;'>` +
		       `<div style='display:flex;align-items:center;gap:4px;'>` +
		       `<span>` + pr.Friendly + `</span>` +
		       `<span class='info-icon' tabindex='0' style='display:inline-flex;align-items:center;justify-content:center;width:16px;height:16px;cursor:pointer;outline:none;'>` +
		       `<svg viewBox='0 0 16 16' width='14' height='14' style='display:block;'><circle cx='8' cy='8' r='7' stroke='#1976d2' stroke-width='2' fill='white'/><text x='8' y='12' text-anchor='middle' font-size='10' fill='#1976d2' font-family='Arial' font-weight='bold'>?</text></svg>` +
			`<span class='info-tooltip' style='display:none;position:absolute;z-index:10;left:22px;top:50%;transform:translateY(-50%);background:#e3f2fd;color:#102a43;padding:7px 12px;border-radius:6px;box-shadow:0 2px 8px rgba(0,0,0,0.08);font-size:13px;min-width:360px;max-width:720px;white-space:normal;overflow:visible;word-break:normal;overflow-wrap:break-word;'>` + htmlpkg.EscapeString(pr.Desc) + `</span>` +
		       `</span>` +
		       `</div></td>`
		       // Adiciona JS/CSS para tooltip interrogação (apenas uma vez, mas seguro repetir)
		       html += `<style>
		       .info-icon:focus .info-tooltip,
		       .info-icon:hover .info-tooltip {
			       display: block;
		       }
		       .info-icon {
			       outline: none;
		       }
			\.info-tooltip {
							transition: opacity 0.15s;
							white-space: normal;
							overflow: visible;
							max-width: 520px;
							word-break: normal;
							overflow-wrap: break-word;
						}
		       </style>
		       <script>
		       function setupInfoTooltips(){
			 document.querySelectorAll('.info-icon').forEach(function(icon){
			       if(icon._tooltipBound) return;
			       icon._tooltipBound = true;
			       icon.addEventListener('click',function(e){
				 var tip = this.querySelector('.info-tooltip');
				 if(tip){ tip.style.display = (tip.style.display==='block') ? 'none' : 'block'; }
				 e.stopPropagation();
			       });
			 });
		       }
		       setupInfoTooltips();
		       document.addEventListener('click',function(){
			 document.querySelectorAll('.info-tooltip').forEach(function(tip){ tip.style.display='none'; });
		       });
		       // Se usar SPA ou renderização dinâmica, chame setupInfoTooltips() após atualizar a tabela
		       </script>`
		       if pr.Err {
			       html += `<tr>` + nameCell + `<td colspan='4'>Erro ao obter dados</td></tr>`
			       continue
		       }
		       if pr.Disabled {
			       dm := "Processo não habilitado"
			       if pr.DisabledMsg != "" { dm = pr.DisabledMsg }
			       html += `<tr>` + nameCell + `<td>-</td><td>-</td><td>-</td><td style='background:#cccccc;color:#000;padding:6px;border-radius:4px;text-align:center;'>` + htmlpkg.EscapeString(dm) + `</td></tr>`
			       continue
		       }
		       html += `<tr>` + nameCell + `<td>` + pr.Smin + `</td><td>` + pr.Savg + `</td><td>` + pr.Smax + `</td><td style='` + pr.StatusStyle + `'>` + pr.StatusText + `</td></tr>`
	       }
	html += `</tbody></table></div>`
	// Procura chave de processo no Zabbix server e pega últimos trend stats (now-30d .. now)
	serverHost = os.Getenv("ZABBIX_SERVER_HOSTID")
	if serverHost == "" {
		log.Printf("[DEBUG] ZABBIX_SERVER_HOSTID not set; searching without hostid for internal processes")
	} else {
		log.Printf("[DEBUG] ZABBIX_SERVER_HOSTID=%s will be used for internal processes", serverHost)
	}
	// use a simple list of process names and build the zabbix[process,...] key dynamically
	procNames := []string{
		"configuration syncer",
		"alerter",
		"alert manager",
		"availability manager",
		"escalator",
		"history syncer",
		"housekeeper",
		"trapper",
		"ipmi manager",
		"lld manager",
		"lld worker",
		"preprocessing manager",
		"preprocessing worker",
		"report manager",
		"report writer",
		"self-monitoring",
		"service manager",
		"task manager",
		"timer",
		"trigger housekeeper",
		"vmware collector",
		"ha manager",
	}
	html += titleWithInfo("h3", "Internal Process", `Os processos internos são responsáveis pelo processamento de informações do servidor e impactam o desempenho dos serviços. Para otimizar, aumente gradualmente o número de processos degradados no arquivo zabbix_server.conf. As decisões de ajuste devem basear-se nas tendências dos últimos ` + checkTrendDisplay + `: se a utilização média estiver consistentemente entre 50% e 60% e os picos ultrapassarem 60%, considere aumentar os pollers/processos; se estiver abaixo de 50%, normalmente não há necessidade de aumento.`)
	html += `<div class='table-responsive'><table class='modern-table'><thead><tr><th>Internal Process</th><th>value_min</th><th>value_avg</th><th>value_max</th><th>Status</th></tr></thead><tbody>`
	// procDesc
	type procRow struct{
		Friendly string
		Desc string
		Smin string
		Savg string
		Smax string
		Vmax float64
		Vavg float64
		StatusText string
		StatusStyle string
		Disabled bool
		DisabledMsg string
		Err bool
	}
	procRows := []procRow{}
	for _, name := range procNames {
		// friendly name (title case), with special-case for LLD
		words := strings.Fields(strings.TrimSpace(name))
		for i, w := range words { words[i] = strings.Title(strings.TrimSpace(w)) }
		if len(words) > 0 && strings.ToLower(words[0]) == "lld" { words[0] = "LLD" }
		friendly := strings.Join(words, " ") + " Internal Processes"

		baseName := strings.ToLower(strings.TrimSpace(name))
		desc := procDesc[baseName]
		if desc == "" { desc = "Internal process" }

		pr := procRow{Friendly: friendly, Desc: desc, Disabled: false, Err: false, Vmax: -1}

		pk := fmt.Sprintf("zabbix[process,%s,avg,busy]", name)
		item, ierr := getItemByKey(apiUrl, token, pk, serverHost)
		if ierr != nil {
			pr.Err = true
			procRows = append(procRows, pr)
			continue
		}
		if item == nil {
			pr.Disabled = true
			if serverHost != "" {
				// verify whether the host id actually exists in Zabbix
				hostExists := false
				hostParams := map[string]interface{}{"output": []string{"hostid"}, "hostids": []string{serverHost}}
				if hresp, herr := zabbixApiRequest(apiUrl, token, "host.get", hostParams); herr == nil {
					if rr, ok := hresp["result"]; ok {
						if arr, ok2 := rr.([]interface{}); ok2 && len(arr) > 0 {
							hostExists = true
						}
					}
				}
				if !hostExists {
					pr.DisabledMsg = fmt.Sprintf("Hostid %s não encontrado, informe o valor na ENV ZABBIX_SERVER_HOSTID.", serverHost)
				} else {
					// host exists but item not found: either not enabled or not present in this Zabbix version
					if majorV < 7 {
						n := strings.ToLower(strings.TrimSpace(name))
						if n == "lld manager" || n == "lld worker" {
							pr.DisabledMsg = "Não existe nesta versão do Zabbix"
						} else {
							pr.DisabledMsg = "Processo não habilitado"
						}
					} else {
						pr.DisabledMsg = "Processo não habilitado"
					}
				}
			} else {
				if majorV < 7 {
					n := strings.ToLower(strings.TrimSpace(name))
					if n == "lld manager" || n == "lld worker" {
						pr.DisabledMsg = "Não existe nesta versão do Zabbix"
					} else {
						pr.DisabledMsg = "Processo não habilitado"
					}
				} else {
					pr.DisabledMsg = "Processo não habilitado"
				}
			}
			procRows = append(procRows, pr)
			continue
		}
		itemid := fmt.Sprintf("%v", item["itemid"])
		log.Printf("[DEBUG] Found internal process item: key=%s itemid=%s hostid=%v", pk, itemid, item["hostid"])
		trend, terr := getLastTrend(apiUrl, token, itemid, 30)
		if terr != nil {
			pr.Err = true
			procRows = append(procRows, pr)
			continue
		}
		if trend == nil {
			pr.Disabled = true
			pr.DisabledMsg = "Processo não habilitado"
			procRows = append(procRows, pr)
			continue
		}
		parseVal := func(k string) float64 {
			if v, ok := trend[k]; ok {
				s := fmt.Sprintf("%v", v)
				if f, err := strconv.ParseFloat(s, 64); err == nil { return f }
			}
			return -1
		}
		vmin := parseVal("value_min")
		vavg := parseVal("value_avg")
		vmax := parseVal("value_max")
		pr.Vmax = vmax
		pr.Vavg = vavg
		fmtVal := func(f float64) string {
			if f < 0 { return "-" }
			return fmt.Sprintf("%.2f%%", f)
		}
		pr.Smin = fmtVal(vmin)
		pr.Savg = fmtVal(vavg)
		pr.Smax = fmtVal(vmax)
		if vavg >= 0 {
			// Two-state logic: OK if vavg < 49.9%, otherwise Atenção
			if vavg < 49.9 {
				pr.StatusText = "OK"
				pr.StatusStyle = "background:#66c28a;color:#000;padding:6px;border-radius:4px;text-align:center;"
			} else {
				pr.StatusText = "Atenção"
				pr.StatusStyle = "background:#ff6666;color:#000;padding:6px;border-radius:4px;text-align:center;"
			}
		} else {
			pr.StatusText = "-"
		}
		procRows = append(procRows, pr)
	}
	// sort by Vmax desc
	sort.Slice(procRows, func(i, j int) bool { return procRows[i].Vavg > procRows[j].Vavg })
	// render
	for _, pr := range procRows {
		nameCell := `<td style='position:relative;padding:0;'>` +
		`<div style='display:flex;align-items:center;gap:4px;'>` +
		`<span>` + pr.Friendly + `</span>` +
		`<span class='info-icon' tabindex='0' style='display:inline-flex;align-items:center;justify-content:center;width:16px;height:16px;cursor:pointer;outline:none;'>` +
		`<svg viewBox='0 0 16 16' width='14' height='14' style='display:block;'><circle cx='8' cy='8' r='7' stroke='#1976d2' stroke-width='2' fill='white'/><text x='8' y='12' text-anchor='middle' font-size='10' fill='#1976d2' font-family='Arial' font-weight='bold'>?</text></svg>` +
		`<span class='info-tooltip' style='display:none;position:absolute;z-index:10;left:22px;top:50%;transform:translateY(-50%);background:#e3f2fd;color:#102a43;padding:7px 12px;border-radius:6px;box-shadow:0 2px 8px rgba(0,0,0,0.08);font-size:13px;min-width:360px;max-width:720px;white-space:normal;overflow:visible;word-break:normal;overflow-wrap:break-word;'>` + htmlpkg.EscapeString(pr.Desc) + `</span>` +
		`</span>` +
		`</div></td>`
			// Adiciona JS/CSS para tooltip interrogação
			html += `<style>
			.info-icon:focus .info-tooltip,
			.info-icon:hover .info-tooltip {
				display: block;
			}
			.info-icon {
				outline: none;
			}
			\.info-tooltip {
							transition: opacity 0.15s;
							white-space: normal;
							overflow: visible;
							max-width: 520px;
							word-break: normal;
							overflow-wrap: break-word;
						}
			</style>
			<script>
			function setupInfoTooltips(){
			  document.querySelectorAll('.info-icon').forEach(function(icon){
				if(icon._tooltipBound) return;
				icon._tooltipBound = true;
				icon.addEventListener('click',function(e){
				  var tip = this.querySelector('.info-tooltip');
				  if(tip){ tip.style.display = (tip.style.display==='block') ? 'none' : 'block'; }
				  e.stopPropagation();
				});
			  });
			}
			setupInfoTooltips();
			document.addEventListener('click',function(){
			  document.querySelectorAll('.info-tooltip').forEach(function(tip){ tip.style.display='none'; });
			});
			// Se usar SPA ou renderização dinâmica, chame setupInfoTooltips() após atualizar a tabela
			</script>`
		if pr.Err {
			html += `<tr>` + nameCell + `<td colspan='4'>Erro ao obter dados</td></tr>`
			continue
		}
		if pr.Disabled {
			dm := "Processo não habilitado"
			if pr.DisabledMsg != "" { dm = pr.DisabledMsg }
			html += `<tr>` + nameCell + `<td>-</td><td>-</td><td>-</td><td style='background:#cccccc;color:#000;padding:6px;border-radius:4px;text-align:center;'>` + htmlpkg.EscapeString(dm) + `</td></tr>`
			continue
		}
		html += `<tr>` + nameCell + `<td>` + pr.Smin + `</td><td>` + pr.Savg + `</td><td>` + pr.Smax + `</td><td style='` + pr.StatusStyle + `'>` + pr.StatusText + `</td></tr>`
	}
	html += `</tbody></table></div>`

	// close processos tab
	html += `</tbody></table></div>`
	html += `</div>` // end tab-processos

	// --- Proxys tab (Zabbix Proxys) ---
	html += `<div id='tab-proxys' class='tab-panel' style='display:none;'>`
	html += titleWithInfo("h3", "Sumário Zabbix Proxys", "De preferencia para proxys Ativos. Proxys Passivos podem ser usados, em casos especificos, requer que o Zabbix Server consiga iniciar conexões com o Proxy. Verifique se os proxys estão atualizados e configurados corretamente.")
	// Small summary table for proxies (unknown / offline / active / passive / total) placed above details
	unknown := 0
	offline := 0
	active := 0
	passive := 0
	total := 0
	if len(proxies) > 0 {
		for _, p := range proxies {
			// Prefer 'state' (newer API) but fall back to 'status' when absent
			stVal := fmt.Sprintf("%v", p["state"])
			stAlt := fmt.Sprintf("%v", p["status"])
			if stVal == "" { stVal = stAlt }
			// count status-based categories (0=Unknown, 1=Offline)
			if stVal == "0" { unknown++ }
			if stVal == "1" { offline++ }

			// determine active/passive depending on Zabbix major version
			if majorV >= 7 {
				om := fmt.Sprintf("%v", p["operating_mode"])
				if om == "0" { active++ } else if om == "1" { passive++ }
			} else {
				// older Zabbix used different status codes for proxy running state
				st2 := fmt.Sprintf("%v", p["status"])
				if st2 == "5" { active++ } else if st2 == "6" { passive++ }
			}
		}
		total = len(proxies)
	} else {
		// if proxies not available, fall back to proxyCount for total
		total = proxyCount
	}
	// render small table above the proxys details (ordered as requested)
	html += `<div class='table-responsive'><table class='modern-table'><colgroup><col style='width:75%'><col style='width:25%'></colgroup><thead><tr><th>Descrição</th><th>Quantidade</th></tr></thead><tbody>`
	html += `<tr><td>Proxys Unknown</td><td>` + fmt.Sprintf("%d", unknown) + `</td></tr>`
	html += `<tr><td>Proxys Offline</td><td>` + fmt.Sprintf("%d", offline) + `</td></tr>`
	html += `<tr><td>Proxys Ativos</td><td>` + fmt.Sprintf("%d", active) + `</td></tr>`
	html += `<tr><td>Proxys Passivos</td><td>` + fmt.Sprintf("%d", passive) + `</td></tr>`
	html += `<tr><td>Total de Proxys</td><td>` + fmt.Sprintf("%d", total) + ` &nbsp; <a href='` + ambienteUrl + `/zabbix.php?action=proxy.list' target='_blank'>Abrir lista de Proxys</a></td></tr>`
	html += `</tbody></table></div>`

	// Proxys details table (list)
	// show only communicating proxies (state == 2) in the details list
	visibleProxies := []map[string]interface{}{}
	if len(proxies) > 0 {
		for _, p := range proxies {
			st := fmt.Sprintf("%v", p["state"])
			if st == "" { st = fmt.Sprintf("%v", p["status"]) }
			if st == "2" {
				visibleProxies = append(visibleProxies, p)
			}
		}
	}
	if len(visibleProxies) > 0 {
		html += `<h4>Proxys</h4>`
		html += `<div class='table-responsive'><table class='modern-table'><colgroup><col style='width:50%'><col style='width:12%'><col style='width:12%'><col style='width:12%'><col style='width:14%'></colgroup><thead><tr><th>Proxy</th><th>Tipo</th><th>Total de Itens</th><th>Items não suportados</th><th>Queue-10m</th></tr></thead><tbody>`
		for _, p := range visibleProxies {
			name := fmt.Sprintf("%v", p["name"])
			if name == "<nil>" || name == "" { name = fmt.Sprintf("%v", p["host"]) }
			proxyid := fmt.Sprintf("%v", p["proxyid"])
			tipo := "Desconhecido"
			if majorV >= 7 {
				om := fmt.Sprintf("%v", p["operating_mode"])
				if om == "0" { tipo = "Active" } else if om == "1" { tipo = "Passive" } else { tipo = om }
			} else {
				st := fmt.Sprintf("%v", p["status"])
				if st == "5" { tipo = "Active" } else if st == "6" { tipo = "Passive" } else { tipo = st }
			}

			// Preparar valores por proxy
			queueVal := "-"
			itemsUnsupportedVal := "-"
			totalItemsVal := "-"

			// Chamada item.get por proxy (mantém a lista de keys pesquisadas; usamos output extend para obter lastvalue)
			paramsItems := map[string]interface{}{
				"search": map[string]interface{}{"key_": []string{
					"*items_unsupported*",
					"*configuration syncer*",
					"*queue,10m*",
					"*data sender*",
					"*availability manager*",
					"*agent poller*",
					"*browser poller*",
					"*discovery manager*",
					"*discovery worker*",
					"*history syncer*",
					"*housekeeper*",
					"*http agent poller*",
					"*http poller*",
					"*icmp pinger*",
					"*internal poller*",
					"*ipmi manager*",
					"*ipmi poller*",
					"*java poller*",
					"*odbc poller*",
					"*poller*",
					"*preprocessing manager*",
					"*preprocessing worker*",
					"*self-monitoring*",
					"*snmp poller*",
					"*snmp trapper*",
					"*task manager*",
					"*trapper*",
					"*unreachable poller*",
					"*vmware collector*",
				}},
				"searchWildcardsEnabled": true,
				"searchByAny": true,
				"monitored": true,
				"proxyids": proxyid,
				"output": "extend",
			}

			if respItems, ierr := zabbixApiRequest(apiUrl, token, "item.get", paramsItems); ierr == nil {
				if r, ok := respItems["result"]; ok {
					if arr, ok2 := r.([]interface{}); ok2 {
						for _, it := range arr {
							if m, mok := it.(map[string]interface{}); mok {
								key := fmt.Sprintf("%v", m["key_"])
								if strings.Contains(key, "queue,10m") || strings.HasPrefix(key, "zabbix[queue,10m") {
									if lv, lok := m["lastvalue"]; lok {
										queueVal = fmt.Sprintf("%v", lv)
									}
								}
								if strings.Contains(key, "items_unsupported") || key == "zabbix[items_unsupported]" {
									if lv, lok := m["lastvalue"]; lok {
										itemsUnsupportedVal = fmt.Sprintf("%v", lv)
									}
								}
							}
						}
					}
				}
			} else {
				log.Printf("[DEBUG] item.get for proxy %s failed: %v", proxyid, ierr)
			}

			// Chamada separada para total de itens por proxy (countOutput)
			paramsTotal := map[string]interface{}{
				"output": "extend",
				"templated": false,
				"countOutput": true,
				"proxyids": proxyid,
			}
			if respTotal, terr := zabbixApiRequest(apiUrl, token, "item.get", paramsTotal); terr == nil {
				if r, ok := respTotal["result"]; ok {
					totalItemsVal = fmt.Sprintf("%v", r)
				}
			} else {
				log.Printf("[DEBUG] item.get (total) for proxy %s failed: %v", proxyid, terr)
			}

			html += `<tr data-proxyid='` + htmlpkg.EscapeString(proxyid) + `'><td>` + htmlpkg.EscapeString(name) + `</td><td>` + htmlpkg.EscapeString(tipo) + `</td><td style='text-align:center;'>` + htmlpkg.EscapeString(totalItemsVal) + `</td><td style='text-align:center;'>` + htmlpkg.EscapeString(itemsUnsupportedVal) + `</td><td style='text-align:center;'>` + htmlpkg.EscapeString(queueVal) + `</td></tr>`
		}
		html += `</tbody></table></div>`
	} else {
		html += `<div class='como-corrigir'>Nenhum proxy configurado ou informação indisponível.</div>`
	}

	html += titleWithInfo("h3", "Processos e Threads Zabbix Proxys", "Os Zabbix Proxys possuem processos próprios que coletam e encaminham dados ao servidor. Verifique conexões, filas e consumo de recursos por proxy. Use a página de Proxies no frontend para detalhes por proxy.")

	html += `</div>` // end tab-proxys

	// --- Items tab (Itens não suportados + Intervalo de Coleta) ---
	if progressCb != nil { progressCb("Coletando informações de Items sem Template e Itens não suportados...") }
	html += `<div id='tab-items' class='tab-panel' style='display:none;'>`
	// --- Itens não suportados (nova categoria) ---
	// Choose the frontend path depending on Zabbix major version (>=7 use zabbix.php, older use items.php)
	itemsPath := ""
	if majorV >= 7 {
		itemsPath = "zabbix.php?action=item.list&context=host&filter_name=&filter_key=&filter_type=-1&filter_value_type=-1&filter_history=&filter_trends=&filter_delay=&filter_evaltype=0&filter_tags%5B0%5D%5Btag%5D=&filter_tags%5B0%5D%5Boperator%5D=0&filter_tags%5B0%5D%5Bvalue%5D=&filter_state=1&filter_with_triggers=-1&filter_inherited=-1&filter_discovered=-1&filter_set=1"
	} else {
		itemsPath = "items.php?context=host&filter_name=&filter_key=&filter_type=-1&filter_value_type=-1&filter_snmp_oid=&filter_history=&filter_trends=&filter_delay=&filter_evaltype=0&filter_tags%5B0%5D%5Btag%5D=&filter_tags%5B0%5D%5Boperator%5D=0&filter_tags%5B0%5D%5Bvalue%5D=&filter_state=1&filter_with_triggers=-1&filter_inherited=-1&filter_discovered=-1&filter_set=1"
	}

	// --- Items sem Template (moved here: items created at host level, not from templates) ---
	itemsNoTplCount = 0
	paramsNoTpl := map[string]interface{}{
		"output": "extend",
		"filter": map[string]interface{}{"flags": 0},
		"countOutput": true,
		"templated": false,
		"inherited": false,
	}
	if respNoTpl, errNoTpl := zabbixApiRequest(apiUrl, token, "item.get", paramsNoTpl); errNoTpl == nil {
		if r, ok := respNoTpl["result"]; ok {
			switch v := r.(type) {
			case float64:
				itemsNoTplCount = int(v)
			case int:
				itemsNoTplCount = v
			default:
				if v2, err := strconv.Atoi(fmt.Sprintf("%v", v)); err == nil { itemsNoTplCount = v2 }
			}
		}
	}
	// build link for items without template
	var itemsNoTplPath string
	if majorV >= 7 {
		itemsNoTplPath = "zabbix.php?action=item.list&context=host&filter_name=&filter_key=&filter_type=-1&filter_value_type=-1&filter_history=&filter_trends=&filter_delay=&filter_evaltype=0&filter_tags%5B0%5D%5Btag%5D=&filter_tags%5B0%5D%5Boperator%5D=0&filter_tags%5B0%5D%5Bvalue%5D=&filter_state=-1&filter_status=-1&filter_with_triggers=-1&filter_inherited=0&filter_discovered=0&filter_set=1"
	} else {
		itemsNoTplPath = "items.php?context=host&filter_name=&filter_key=&filter_type=-1&filter_value_type=-1&filter_snmp_oid=&filter_history=&filter_trends=&filter_delay=&filter_evaltype=0&filter_tags%5B0%5D%5Btag%5D=&filter_tags%5B0%5D%5Boperator%5D=0&filter_tags%5B0%5D%5Bvalue%5D=&filter_state=-1&filter_status=-1&filter_with_triggers=-1&filter_inherited=0&filter_discovered=0&filter_set=1"
	}
	itemsNoTplLink := ambienteUrl + "/" + itemsNoTplPath

	unsupportedLink := ambienteUrl + "/" + itemsPath
	html += titleWithInfo("h3", "Items sem Template", "Item sem template não afeta performance de Processos do Zabbix, porem para melhor organização é importante um item ter template e não ser criado a nivel de Host.")
	if itemsNoTplCount > 0 {
		html += `<div class='table-responsive'><table class='modern-table'><thead><tr><th>Descrição</th><th>Quantidade</th><th>Link</th></tr></thead><tbody>`
		html += `<tr><td>Itens sem Template</td><td>` + fmt.Sprintf("%d", itemsNoTplCount) + `</td><td><a href='` + itemsNoTplLink + `' target='_blank'>Abrir</a></td></tr>`
		html += `</tbody></table></div>`
	} else {
		html += ``
	}

	html += titleWithInfo("h3", "Itens não suportados", "Como corrigir: " + descNaoSuportados)
	// (legend use .como-corrigir)
	html += `<div class='table-responsive'><table class='modern-table'><thead><tr><th>Tipo de Item</th><th>Total</th><th>Não suportados</th><th>Link</th></tr></thead><tbody>`

	// Define item types to query (type code -> label)
	baseTypes := []struct{ Code int; Label string }{
		{0, "Zabbix Agent"},
		{2, "Zabbix Trapper"},
		{3, "Simple check"},
		{5, "Zabbix internal"},
		{7, "Zabbix agent (active)"},
		{8, "Aggregate"},
		{9, "Web item"},
		{10, "External check"},
		{11, "Database"},
		{12, "IPMI"},
		{13, "SSH"},
		{14, "Telnet"},
		{15, "Calculated"},
		{16, "JMX"},
		{17, "SNMP Trap"},
		{18, "Dependent item"},
		{19, "HTTP agent"},
		{20, "SNMP"},
		{21, "Script"},
	}
	types := baseTypes
	// Include Browser (22) only when Zabbix major version is 7 or newer
	if majorV >= 7 {
		types = append(types, struct{ Code int; Label string }{22, "Browser"})
	}

	// If Zabbix < 7, browser (22) is not supported in frontend listing; we skip showing it
	// collect rows so we can sort by unsupported count
	type rowT struct{ Label string; Total int; Unsup int; Link string }

	// concurrency-limited worker pool (configurable via env MAX_CCONCURRENT)
	maxConcurrent := 6
	if v := os.Getenv("MAX_CCONCURRENT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 { maxConcurrent = n }
	}
	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup
	resultsCh := make(chan rowT, len(types))

	for _, t := range types {
		t := t
		wg.Add(1)
		go func(tt struct{ Code int; Label string }) {
			defer wg.Done()
			sem <- struct{}{}
			defer func(){ <-sem }()

			// build params for total count of this type
			paramsTotal := map[string]interface{}{
				"output": "extend",
				"filter": map[string]interface{}{ "type": tt.Code },
				"templated": false,
				"countOutput": true,
				"monitored": true,
			}
			// build params for unsupported count of this type (state = 1)
			paramsUnsup := map[string]interface{}{
				"output": "extend",
				"filter": map[string]interface{}{ "type": tt.Code, "state": 1 },
				"templated": false,
				"countOutput": true,
				"monitored": true,
			}

			cntTotal := 0
			cntUnsup := 0

			if respTotal, errTotal := zabbixApiRequest(apiUrl, token, "item.get", paramsTotal); errTotal == nil {
				if r, ok := respTotal["result"]; ok {
					switch v := r.(type) {
					case float64:
						cntTotal = int(v)
					case int:
						cntTotal = v
					default:
						if v2, err := strconv.Atoi(fmt.Sprintf("%v", v)); err == nil { cntTotal = v2 }
					}
				}
			}
			if respUns, errUns := zabbixApiRequest(apiUrl, token, "item.get", paramsUnsup); errUns == nil {
				if r, ok := respUns["result"]; ok {
					switch v := r.(type) {
					case float64:
						cntUnsup = int(v)
					case int:
						cntUnsup = v
					default:
						if v2, err := strconv.Atoi(fmt.Sprintf("%v", v)); err == nil { cntUnsup = v2 }
					}
				}
			}

			perPath := strings.Replace(itemsPath, "filter_type=-1", fmt.Sprintf("filter_type=%d", tt.Code), 1)
			perLink := ambienteUrl + "/" + perPath
			linkHTML := ""
			if tt.Code == 22 && majorV < 7 {
				linkHTML = "-"
			} else {
				linkHTML = "<a href='" + perLink + "' target='_blank'>Abrir</a>"
			}

			resultsCh <- rowT{Label: tt.Label, Total: cntTotal, Unsup: cntUnsup, Link: linkHTML}
		}(t)
	}

	wg.Wait()
	close(resultsCh)

	rows := []rowT{}
	for r := range resultsCh { rows = append(rows, r) }

	// sort by Unsup desc
	sort.Slice(rows, func(i, j int) bool { return rows[i].Unsup > rows[j].Unsup })

	for _, r := range rows {
		if r.Unsup == 0 {
			continue
		}
		html += `<tr><td>` + r.Label + `</td><td>` + fmt.Sprintf("%d", r.Total) + `</td><td>` + fmt.Sprintf("%d", r.Unsup) + `</td><td>` + r.Link + `</td></tr>`
	}

	// final total row: use existing total items query result for total items, and unsupportedVal for not supported total
	totalItemsInt := 0
	if nItemsTotal != "-" {
		if v, err := strconv.Atoi(strings.TrimSpace(nItemsTotal)); err == nil { totalItemsInt = v }
	}
	html += `<tr><td><strong>Total</strong></td><td><strong>` + fmt.Sprintf("%d", totalItemsInt) + `</strong></td><td><strong>` + fmt.Sprintf("%d", unsupportedVal) + `</strong></td><td><a href='` + unsupportedLink + `' target='_blank'>Abrir listagem completa</a></td></tr>`
	html += `</tbody></table></div>`

	// --- Intervalo de Coleta ---
	// Coleta a quantidade de itens com update interval de 1s, 10s, 30s e 60s
	intervals := []int{1, 10, 30, 60}
	intervalRows := []struct{ Interval int; Count int; Link string }{}
	for _, d := range intervals {
		params := map[string]interface{}{
			"output": "extend",
			"filter": map[string]interface{}{"delay": d},
			"templated": false,
			"countOutput": true,
		}
		cnt := 0
		if resp, err := zabbixApiRequest(apiUrl, token, "item.get", params); err == nil {
			if r, ok := resp["result"]; ok {
				switch v := r.(type) {
				case float64:
					cnt = int(v)
				case int:
					cnt = v
				default:
					if v2, err := strconv.Atoi(fmt.Sprintf("%v", v)); err == nil { cnt = v2 }
				}
			}
		}
		// montar link para a listagem com filter_delay (usar path apropriado para versão do Zabbix)
		var perPath string
		if majorV >= 7 {
			perPath = fmt.Sprintf("zabbix.php?action=item.list&context=host&filter_name=&filter_key=&filter_type=-1&filter_value_type=-1&filter_history=&filter_trends=&filter_delay=%d&filter_evaltype=0&filter_tags%%5B0%%5D%%5Btag%%5D=&filter_tags%%5B0%%5D%%5Boperator%%5D=0&filter_tags%%5B0%%5D%%5Bvalue%%5D=&filter_state=-1&filter_with_triggers=-1&filter_inherited=-1&filter_discovered=-1&filter_set=1", d)
		} else {
			perPath = fmt.Sprintf("items.php?context=host&filter_name=&filter_key=&filter_type=-1&filter_value_type=-1&filter_snmp_oid=&filter_history=&filter_trends=&filter_delay=%d&filter_evaltype=0&filter_tags%%5B0%%5D%%5Btag%%5D=&filter_tags%%5B0%%5D%%5Boperator%%5D=0&filter_tags%%5B0%%5D%%5Bvalue%%5D=&filter_state=-1&filter_status=-1&filter_with_triggers=-1&filter_inherited=-1&filter_discovered=-1&filter_set=1", d)
		}
		perLink := ambienteUrl + "/" + perPath
		linkHTML := "<a href='" + perLink + "' target='_blank'>Abrir</a>"
		intervalRows = append(intervalRows, struct{ Interval int; Count int; Link string }{Interval: d, Count: cnt, Link: linkHTML})
	}

	// renderiza a seção de Intervalo de Coleta
	// (legend use .como-corrigir)
	html += titleWithInfo("h3", "Intervalo de Coleta:", "As métricas de monitoramento serão coletadas com base no intervalo de coleta definido no item, quanto menor o intervalo de coleta mais recursos de CPU e memória será utilizado no Zabbix Server e/ou Zabbix Proxy além de relação direta com o crescimento do Banco de Dados, VPS do Zabbix e no processo de Housekeeper. Intervalos Verificados 1, 10, 30, 60.")
	html += `<div class='table-responsive'><table class='modern-table'><thead><tr><th>Intervalo (s)</th><th>Quantidade de itens</th><th>Link</th></tr></thead><tbody>`
	for _, r := range intervalRows {
		if r.Count == 0 {
			continue
		}
		html += `<tr><td>` + fmt.Sprintf("%d", r.Interval) + `</td><td>` + fmt.Sprintf("%d", r.Count) + `</td><td>` + r.Link + `</td></tr>`
	}
	html += `</tbody></table></div>`

	// --- Regras de LLD (Discovery rules) ---
	// Coleta a quantidade de regras de descoberta (discoveryrule) com delay 1s,10s,30s,60s,300s
	lldIntervals := []int{1, 10, 30, 60, 300}
	lldRows := []struct{ Interval int; Count int; Link string }{}
	for _, d := range lldIntervals {
		params := map[string]interface{}{
			"output": "extend",
			"filter": map[string]interface{}{"delay": d},
			"templated": true,
			"countOutput": true,
		}
		cnt := 0
		if resp, err := zabbixApiRequest(apiUrl, token, "discoveryrule.get", params); err == nil {
			if r, ok := resp["result"]; ok {
				switch v := r.(type) {
				case float64:
					cnt = int(v)
				case int:
					cnt = v
				default:
					if v2, err := strconv.Atoi(fmt.Sprintf("%v", v)); err == nil { cnt = v2 }
				}
			}
		}

		// montar link para a listagem de discovery rules com filter_delay (usar path apropriado para versão do Zabbix)
		// formatar delay como "Ns" ou "Nm" para o link (Zabbix frontend espera p.ex. 5m)
		delayFmt := ""
		if d >= 60 && d%60 == 0 {
			delayFmt = fmt.Sprintf("%dm", d/60)
		} else {
			delayFmt = fmt.Sprintf("%ds", d)
		}

		var perPath string
		if majorV >= 7 {
			perPath = fmt.Sprintf("host_discovery.php?context=template&filter_name=&filter_key=&filter_type=-1&filter_delay=%s&filter_lifetime_type=-1&filter_enabled_lifetime_type=-1&filter_snmp_oid=&filter_status=-1&filter_set=1", delayFmt)
		} else {
			perPath = fmt.Sprintf("host_discovery.php?context=template&filter_name=&filter_key=&filter_type=-1&filter_delay=%s&filter_lifetime=&filter_snmp_oid=&filter_status=-1&filter_set=1", delayFmt)
		}
		perLink := ambienteUrl + "/" + perPath
		linkHTML := "<a href='" + perLink + "' target='_blank'>Abrir</a>"

		lldRows = append(lldRows, struct{ Interval int; Count int; Link string }{Interval: d, Count: cnt, Link: linkHTML})
	}

	// renderiza a seção de Regras de LLD (Intervalo de Coleta)
	html += titleWithInfo("h3", "Regras de LLD (Discovery rules) - Intervalo de Coleta:", "As métricas de monitoramento serão coletadas com base no intervalo de coleta definido na regra LLD; quanto menor o intervalo, maior o consumo de CPU/memória e crescimento do banco. Verifique se intervalos muito curtos são realmente necessários.")
	// legend moved into tooltip via titleWithInfo
	html += `<div class='table-responsive'><table class='modern-table'><thead><tr><th>Intervalo (s)</th><th>Quantidade de regras</th><th>Link</th></tr></thead><tbody>`
	for _, r := range lldRows {
		if r.Count == 0 {
			continue
		}
		html += `<tr><td>` + fmt.Sprintf("%d", r.Interval) + `</td><td>` + fmt.Sprintf("%d", r.Count) + `</td><td>` + r.Link + `</td></tr>`
	}
	html += `</tbody></table></div>`

	// --- Regras de LLD (Discovery rules) - Not Supported ---
	// Conta discovery rules com state = 1 (Not supported / erro) no escopo de hosts (não templated)
	lldNotSupCnt := 0
	paramsNotSup := map[string]interface{}{
		"output": "extend",
		"filter": map[string]interface{}{"state": 1},
		"templated": false,
		"countOutput": true,
	}
	if resp, err := zabbixApiRequest(apiUrl, token, "discoveryrule.get", paramsNotSup); err == nil {
		if r, ok := resp["result"]; ok {
			switch v := r.(type) {
			case float64:
				lldNotSupCnt = int(v)
			case int:
				lldNotSupCnt = v
			default:
				if v2, err := strconv.Atoi(fmt.Sprintf("%v", v)); err == nil { lldNotSupCnt = v2 }
			}
		}
	}

	// montar link para a listagem de discovery rules com filter_state=1 (usar path apropriado para versão do Zabbix)
	var lldPerPath string
	if majorV >= 7 {
		lldPerPath = "host_discovery.php?context=host&filter_name=&filter_key=&filter_type=-1&filter_delay=&filter_lifetime_type=-1&filter_enabled_lifetime_type=-1&filter_snmp_oid=&filter_state=1&filter_set=1"
	} else {
		lldPerPath = "host_discovery.php?context=host&filter_name=&filter_key=&filter_type=-1&filter_delay=&filter_lifetime=&filter_snmp_oid=&filter_state=1&filter_set=1"
	}
	lldPerLink := ambienteUrl + "/" + lldPerPath

	if lldNotSupCnt > 0 {
		html += titleWithInfo("h3", "Regras de LLD (Discovery rules) - Not Supported", "Regras não suportadas são aquelas que apresentam erro ao executar a coleta. Essas regras continuam consumindo processos do Zabbix desnecessariamente; valide os erros e corrija ou desative regras inválidas.")
		// legend moved into tooltip via titleWithInfo
		html += `<div class='table-responsive'><table class='modern-table'><thead><tr><th>Descrição</th><th>Quantidade</th><th>Link</th></tr></thead><tbody>`
		html += `<tr><td>Regras de descoberta com status de erro</td><td>` + fmt.Sprintf("%d", lldNotSupCnt) + `</td><td><a href='` + lldPerLink + `' target='_blank'>Abrir</a></td></tr>`
		html += `</tbody></table></div>`
	}

	// close items tab
	html += `</div>` // end tab-items

	// --- Templates tab ---
	html += `<div id='tab-templates' class='tab-panel' style='display:none;'>`
	// Detalhamento dos Principais Templates
	html += titleWithInfo("h3", "Detalhamento dos Principais Templates", descDetalhamento)
		// legend moved into tooltip via titleWithInfo
	for _, tpl := range topTemplates {
		tplName := templateNames[tpl.Key]
		if tplName == "" { tplName = tpl.Key }
		html += `<h4>` + tplName + `</h4>`
		html += `<div class='table-responsive'><table class='modern-table'><thead><tr><th>Item</th><th>Erro</th><th>Host</th><th>Link</th></tr></thead><tbody>`
		rows := templateItems[tpl.Key]
		for i, row := range rows {
			if i >= topN { break }
			html += `<tr><td>` + row[0] + `</td><td>` + row[1] + `</td><td>` + row[2] + `</td><td><a href='` + row[3] + `' target='_blank'>Editar</a></td></tr>`
		}
		html += `</tbody></table></div>`
	}
	// close templates tab and main container
	html += `</div>` // end tab-templates

	// Recomendações tab (espaço para sugestões automáticas / ações)
	html += `<div id='tab-recomendacoes' class='tab-panel' style='display:none;'>`
	html += titleWithInfo("h3", "Recomendações", "Sugestões geradas automaticamente com base no relatório. Use como ponto de partida para investigações e correções.")
	html += `<div style='background:#e8f5e9;padding:10px;border-radius:6px;margin-bottom:8px;'>Sugestões geradas automaticamente com base no relatório. Use como ponto de partida para investigações e correções.</div>`

	// --- Recomendações dinâmicas ---
	// 1) Processos e Threads: combine pollers + internal processes with status Atenção
	attention := []struct{ Name string; Vavg float64 }{}
	for _, pr := range pollRows {
		if pr.StatusText == "Atenção" && pr.Vavg >= 0 {
			attention = append(attention, struct{ Name string; Vavg float64 }{pr.Friendly, pr.Vavg})
		}
	}
	for _, pr := range procRows {
		if pr.StatusText == "Atenção" && pr.Vavg >= 0 {
			attention = append(attention, struct{ Name string; Vavg float64 }{pr.Friendly, pr.Vavg})
		}
	}
	// sort by Vavg desc
	sort.Slice(attention, func(i, j int) bool { return attention[i].Vavg > attention[j].Vavg })

	html += `<h4>Processos e Threads</h4>`
	// legend moved into tooltip via titleWithInfo
	if len(attention) == 0 {
		html += `<p>Nenhum processo em estado de Atenção detectado.</p>`
	} else {
		html += `<ol style='margin-left:18px;'>`
		for _, a := range attention {
			html += `<li>` + htmlpkg.EscapeString(a.Name) + ` — média: ` + fmt.Sprintf("%.2f%%", a.Vavg) + `</li>`
		}
		html += `</ol>`
	}

	// 2) Items: report counts already collected (itemsNoTplCount, unsupportedVal, intervalRows, lldRows, nItemsDisabled)
	// Additional process-specific recommendations for Zabbix 7+: Pollers Assíncronos subtopic
	if majorV >= 7 {
		agentDisabled := false
		snmpDisabled := false
		httpAgentDisabled := false
		for _, pr := range pollRows {
			if pr.Disabled && pr.DisabledMsg == "Processo não habilitado" {
				lower := strings.ToLower(pr.Friendly)
				if strings.Contains(lower, "agent poller") {
					agentDisabled = true
				}
				if strings.Contains(lower, "snmp poller") || strings.Contains(lower, "snmp") {
					snmpDisabled = true
				}
				if strings.Contains(lower, "http agent") || strings.Contains(lower, "http agent poller") || strings.Contains(lower, "http") {
					httpAgentDisabled = true
				}
			}
		}
		// only render subtopic if any async poller is disabled
		if agentDisabled || snmpDisabled || httpAgentDisabled {
			html += `<div style='margin-left:6px;'>`
			html += `<h5>Pollers Assíncronos</h5>`
			// legend moved into tooltip via titleWithInfo
			if agentDisabled {
				html += `<p><strong>Habilitar "Agent Poller" no arquivo de configuração zabbix_server.conf (se utilizado proxys, altera no zabbix_proxy.conf):</strong> Processo Asynchronous para verificações passivas, novidade do Zabbix 7.</p>`
			}
			if snmpDisabled {
				html += `<p><strong>Habilitar "Snmp Poller" no arquivo de configuração do zabbix_server.conf (se utilizado proxys, altera no zabbix_proxy.conf):</strong> Processo Asynchronous para verificações de SNMP, novidade do Zabbix 7.</p>`
			}
			if httpAgentDisabled {
				html += `<p><strong>Habilitar "HTTP Agent Poller" no arquivo de configuração do zabbix_server.conf (se utilizado proxys, altera no zabbix_proxy.conf):</strong> Processo Asynchronous para verificações de HTTP, novidade do Zabbix 7.</p>`
			}
			html += `</div>`
		}
	}
	// prepare numeric disabled count
	disabledCount := 0
	if nItemsDisabled != "-" {
		if v, err := strconv.Atoi(strings.TrimSpace(nItemsDisabled)); err == nil { disabledCount = v }
	}
	// percentage helpers
	pct := func(part, total int) string {
		if total <= 0 { return "0%" }
		return fmt.Sprintf("%.2f%%", (float64(part)*100.0)/float64(total))
	}
	// items with interval <= 60s (sum 1,10,30,60)
	itemsLe60 := 0
	for _, r := range intervalRows { if r.Interval <= 60 { itemsLe60 += r.Count } }
	// LLD rules with interval <= 300s (sum 1,10,30,60,300)
	lldLe300 := 0
	for _, r := range lldRows { if r.Interval <= 300 { lldLe300 += r.Count } }

	html += `<h4>Items</h4>`
	html += `<div style='margin-left:6px;'>`
	html += `<p><strong>Existem ` + fmt.Sprintf("%d", itemsNoTplCount) + ` Itens sem Template:</strong> validar a necessidade de criação de template para estes itens, não impacta diretamente na performance do Zabbix, porem é útil para organização e reutilização dos itens.</p>`
	html += `<p><strong>Existem ` + fmt.Sprintf("%d", unsupportedVal) + ` Itens não suportados, cerca de ` + pct(unsupportedVal, totalItemsVal) + ` do total de itens do ambiente:</strong> Os itens não suportados são aqueles que estão ativos, porém no momento de efetuar a coleta/processar a métrica apresentou algum erro. Esses itens continuam consumindo os processos desnecessariamente do Zabbix, causando consumo de recursos de hardware.</p>`
	html += `<p><strong>Existem ` + fmt.Sprintf("%d", disabledCount) + ` itens desabilitados, cerca de ` + pct(disabledCount, totalItemsVal) + ` do total de itens do ambiente:</strong> Os itens desabilitados não consomem os processos do Zabbix, entretanto é necessário avaliar por qual motivo esses itens foram desabilitados, qual o impacto para o monitoramento e ao serviço monitorado.</p>`
	html += `<p><strong>Existem ` + fmt.Sprintf("%d", itemsLe60) + ` Itens com Intervalo de Coleta menor ou igual a 60s:</strong> As métricas de monitoramento serão coletadas com base no intervalo de coleta definido no item, quanto menor o intervalo de coleta mais recursos de CPU e memória será utilizado no Zabbix Server e/ou Zabbix Proxy além de relação direta com o crescimento do Banco de Dados, VPS do Zabbix e no processo de Housekeeper, é interessante avaliar a necessidade.</p>`
	// LLD explanatory paragraph removed from Items tab to avoid duplication; kept in Recomendações
	html += `</div>`	

	// --- Regras de LLD (tópico separado nas Recomendações) ---
	html += `<h4>Regras de LLD</h4>`
	html += `<div style='margin-left:6px;'>`
	html += `<p><strong>Existem ` + fmt.Sprintf("%d", lldLe300) + ` Regras de LLD com Intervalo de Coleta menor ou igual a 300s:</strong> LLD fornecem uma forma automática para criar itens, triggers, gráficos para diferentes objetos de um determinado dispositivo. Muitos casos não há necessidade de ter uma nova descoberta a cada minuto, por exemplo, uma placa de rede não é acrescentada a cada 5min, logo uma regra de LLD de Interface não precisa ter um periodo de Intervalo de Coleta baixo, isso impacta diretamente no Processo Interno LLD Manager.</p>`
	html += `<p><strong>Existem ` + fmt.Sprintf("%d", lldNotSupCnt) + ` Regras de LLD que estão com o Status de não suportados:</strong> Há necessidade de validação para entendimento e correção dos problemas, isso impacta diretamente no Processo Interno LLD Manager.</p>`
	html += `</div>`

	// small JS to handle tab switching (keeps markup simple and UX clean)
	html += `<script>` +
		`function showTab(id){` +
			`var panels=document.querySelectorAll('.tab-panel');` +
			`panels.forEach(function(p){p.style.display='none';});` +
			`var el=document.getElementById(id); if(el) el.style.display='block';` +
			`var btns=document.querySelectorAll('.tab-btn');` +
			`btns.forEach(function(b){b.classList.remove('active');});` +
			`var active=document.querySelector(".tab-btn[data-tab='"+id+"']"); if(active){ active.classList.add('active'); }` +
		`}` +
		`document.querySelectorAll('.tab-btn').forEach(function(b){ b.addEventListener('click', function(){ showTab(this.getAttribute('data-tab')); }); });` +
	`</script>`
	html += `</div>`
	return html, nil
}

func main() {
	// initialize debug flag from ENV
	v := strings.ToLower(strings.TrimSpace(os.Getenv("APP_DEBUG")))
	if v == "1" || v == "true" || v == "yes" {
		debugApi = true
		log.Printf("[DEBUG] APP_DEBUG enabled: verbose API logs ON")
	}
	// initialize trend window from ENV CHECKTRENDTIME
	parseCheckTrendEnv()
	r := gin.Default()

	r.Static("/static", "./web/static")
	r.LoadHTMLGlob("web/templates/*")

	r.GET("/", func(c *gin.Context) {
		c.HTML(http.StatusOK, "index.html", nil)
	})


	       // In-memory task store (substitua por Postgres depois)
		       type Task struct {
			       ID     string
			       Status string // "pending", "processing", "done", "error"
			       Report string
			       ProgressMsg string // mensagem de progresso
		       }
		       var tasks = make(map[string]*Task)

	       r.POST("/api/start", func(c *gin.Context) {
		       type Req struct {
			       ZabbixURL   string `json:"zabbix_url"`
			       ZabbixToken string `json:"zabbix_token"`
		       }
		       var req Req
		       if err := c.ShouldBindJSON(&req); err != nil {
			       log.Printf("[ERROR] Dados inválidos recebidos: %v", err)
			       c.JSON(http.StatusBadRequest, gin.H{"error": "Dados inválidos"})
			       return
		       }
		       log.Printf("[DEBUG] Requisição recebida: url=%s, token=%s", req.ZabbixURL, req.ZabbixToken)
		       id := fmt.Sprintf("task-%d", time.Now().UnixNano())
		       tasks[id] = &Task{ID: id, Status: "processing", ProgressMsg: "Iniciando coleta..."}
		       go func(taskID string, url, token string) {
			       setProgress := func(msg string) {
				       if t, ok := tasks[taskID]; ok { t.ProgressMsg = msg }
			       }
			       setProgress("Detectando versão do Zabbix...")
			       report, err := generateZabbixReportWithProgress(url, token, setProgress)
			       if err != nil {
				       log.Printf("[ERROR] Erro na tarefa %s: %v", taskID, err)
				       tasks[taskID].Status = "error"
				       if strings.Contains(err.Error(), "Not authorized") || strings.Contains(err.Error(), "Not authorised") {
					       tasks[taskID].Report = "<div style='color:red;'>Token Invalido</div>"
				       } else {
					       tasks[taskID].Report = "<div style='color:red;'>Erro: " + err.Error() + "</div>"
				       }
				       return
			       }
			       log.Printf("[DEBUG] Tarefa %s concluída", taskID)
			       tasks[taskID].Status = "done"
			       tasks[taskID].Report = report
			       tasks[taskID].ProgressMsg = "Relatório gerado." // final
		       }(id, req.ZabbixURL, req.ZabbixToken)
			       c.JSON(http.StatusOK, gin.H{"task_id": id})
			       })

				r.GET("/api/progress/:id", func(c *gin.Context) {
					id := c.Param("id")
					task, ok := tasks[id]
					if !ok {
						c.JSON(http.StatusNotFound, gin.H{"error": "Tarefa não encontrada"})
						return
					}
					// Include the optional report HTML so callers can show a meaningful error message
					c.JSON(http.StatusOK, gin.H{"status": task.Status, "progress_msg": task.ProgressMsg, "report": task.Report})
				})

			       r.GET("/api/report/:id", func(c *gin.Context) {
				       id := c.Param("id")
				       task, ok := tasks[id]
				       if !ok || task.Status != "done" {
					       c.JSON(http.StatusNotFound, gin.H{"error": "Relatório não disponível"})
					       return
				       }
				       c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(task.Report))
			       })

			       r.Run(":8080")
			}

			// Wrapper for progress reporting
			func generateZabbixReportWithProgress(url, token string, setProgress func(string)) (string, error) {
				// install callback for use inside generateZabbixReport
				progressCb = setProgress
				// ensure we clear callback when finished
				defer func(){ progressCb = nil }()
				// initial message
				if progressCb != nil { progressCb("Detectando versão do Zabbix...") }
				// Call the original report generator which will call progressCb at key points
				return generateZabbixReport(url, token)
			}
