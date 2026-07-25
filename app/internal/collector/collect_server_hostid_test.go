package collector

import (
	"fmt"
	"testing"
)

func item(hostid, key string) map[string]interface{} {
	return map[string]interface{}{
		"itemid": "1",
		"key_":   key,
		"hosts":  []interface{}{map[string]interface{}{"hostid": hostid, "host": "h" + hostid}},
	}
}

func reqReturning(items ...map[string]interface{}) ApiRequester {
	return func(apiUrl, token, method string, params interface{}) (map[string]interface{}, error) {
		arr := make([]interface{}, 0, len(items))
		for _, it := range items {
			arr = append(arr, it)
		}
		return map[string]interface{}{"result": arr}, nil
	}
}

// Um proxy também tem itens zabbix[process,...]. O host do servidor tem que
// vencer, mesmo quando o proxy aparece primeiro na resposta.
func TestCollectServerHostIdIgnoraProxy(t *testing.T) {
	got, err := CollectServerHostId("u", "t", reqReturning(
		item("20001", "zabbix[process,poller,avg,busy]"),
		item("20001", "zabbix[process,trapper,avg,busy]"),
		item("20001", "zabbix[process,history syncer,avg,busy]"),
		item("10999", "zabbix[process,poller,avg,busy]"),
		item("10999", "zabbix[process,escalator,avg,busy]"),
		item("10999", "zabbix[process,alert manager,avg,busy]"),
	))
	if err != nil {
		t.Fatal(err)
	}
	if got != "10999" {
		t.Fatalf("esperava o host do servidor (10999), veio %q", got)
	}
}

// Sem processo exclusivo de servidor, desempata por quantidade de itens.
func TestCollectServerHostIdDesempataPorTotal(t *testing.T) {
	got, err := CollectServerHostId("u", "t", reqReturning(
		item("1", "zabbix[process,poller,avg,busy]"),
		item("2", "zabbix[process,poller,avg,busy]"),
		item("2", "zabbix[process,trapper,avg,busy]"),
	))
	if err != nil {
		t.Fatal(err)
	}
	if got != "2" {
		t.Fatalf("esperava 2 (mais itens), veio %q", got)
	}
}

// Nenhum item: devolve "" sem erro, para o chamador manter o que já tinha.
func TestCollectServerHostIdSemItens(t *testing.T) {
	got, err := CollectServerHostId("u", "t", reqReturning())
	if err != nil || got != "" {
		t.Fatalf("esperava (\"\", nil), veio (%q, %v)", got, err)
	}
}

// Respostas malformadas não podem derrubar a coleta.
func TestCollectServerHostIdRespostasEstranhas(t *testing.T) {
	cases := map[string]ApiRequester{
		"sem result": func(apiUrl, token, method string, params interface{}) (map[string]interface{}, error) {
			return map[string]interface{}{}, nil
		},
		"result não-array": func(apiUrl, token, method string, params interface{}) (map[string]interface{}, error) {
			return map[string]interface{}{"result": "nada"}, nil
		},
		"item sem hosts": func(apiUrl, token, method string, params interface{}) (map[string]interface{}, error) {
			return map[string]interface{}{"result": []interface{}{
				map[string]interface{}{"itemid": "1", "key_": "zabbix[process,poller,avg,busy]"},
			}}, nil
		},
		"hosts vazio": func(apiUrl, token, method string, params interface{}) (map[string]interface{}, error) {
			return map[string]interface{}{"result": []interface{}{
				map[string]interface{}{"itemid": "1", "key_": "x", "hosts": []interface{}{}},
			}}, nil
		},
	}
	for name, req := range cases {
		got, err := CollectServerHostId("u", "t", req)
		if err != nil || got != "" {
			t.Errorf("%s: esperava (\"\", nil), veio (%q, %v)", name, got, err)
		}
	}
}

func TestCollectServerHostIdPropagaErro(t *testing.T) {
	req := func(apiUrl, token, method string, params interface{}) (map[string]interface{}, error) {
		return nil, fmt.Errorf("boom")
	}
	if _, err := CollectServerHostId("u", "t", req); err == nil {
		t.Fatal("esperava erro propagado")
	}
}
