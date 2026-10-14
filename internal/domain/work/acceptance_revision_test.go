package work

import "testing"

func TestAcceptanceRevisionInstructionUsesOnlyUnambiguousConversationContext(t *testing.T) {
	id := "10000000-0000-4000-8000-000000000001"
	for _, tc := range []struct {
		name, text string
		sole, want bool
	}{
		{"contextual single item", "請把這個 Epic 的第 1 點驗收改成跨 Session 可定位", true, true},
		{"ambiguous several items", "請把這個 Epic 的第 1 點驗收改成跨 Session 可定位", false, false},
		{"named among several", "Revise acceptance for " + id, false, true},
		{"note reply context is not another item", "請修訂 Parent Epic 的驗收：只標示發起人。 (Clawdline 便條 20000000-0000-4000-8000-000000000002：「確認驗收」；待回覆事項：請選擇。)", false, true},
		{"dropping a requirement is still a revision", "請修訂 " + id + " 的驗收條件：不要求額外的測試主機名。 (Clawdline 便條 20000000-0000-4000-8000-000000000002：「確認驗收」；待回覆事項：請選擇。)", false, true},
		{"prohibited behavior in replacement criteria", "請把看板 " + id + " 的驗收修訂為：已接受操作不得默默遺失；付款不列入。", false, true},
		{"prohibition before replacement criteria", "請不要把看板 " + id + " 的驗收修訂為：已接受操作不得默默遺失。", false, false},
		{"colon before prohibition is not replacement", "請注意：不要修改 " + id + " 的驗收", false, false},
		{"a requirement can be removed without a colon", "請把 " + id + " 的驗收改為不要求額外網址", false, true},
		{"switch this cycle and defer a separate plan", "『" + id + " 本輪改以公開安裝後的 App／Agent／首個 Session 驗收；DMG 留在未來 Plan 40000000-0000-4000-8000-000000000004。』 (Clawdline 便條 20000000-0000-4000-8000-000000000002：「請直接修訂驗收」；待回覆事項：請傳送修訂指示。)", false, true},
		{"short item id with sole ownership and future plan", "『10000000 本輪改以公開安裝後的 App／Agent／首個 Session 驗收；DMG 留在未來 Plan 40000000-0000-4000-8000-000000000004。』 (Clawdline 便條 20000000-0000-4000-8000-000000000002：「請直接修訂 10000000 本輪驗收」；待回覆事項：請傳送修訂指示。)", true, true},
		{"short item id with several owned items stays ambiguous", "10000000 本輪改以首個 Session 驗收；DMG 留在未來 Plan 40000000-0000-4000-8000-000000000004。", false, false},
		{"foreign item in a second clause is still ambiguous", "請把 " + id + " 的驗收改以首個 Session 為準；另外修改 30000000-0000-4000-8000-000000000003 的驗收。", false, false},
		{"future plan alone does not identify this item", "本輪改以首個 Session 驗收；DMG 留在未來 Plan 40000000-0000-4000-8000-000000000004。", false, false},
		{"English removal is not a denial", "Please revise acceptance for " + id + ": do not require a separate domain", false, true},
		{"foreign item in reply still refuses", "請修訂 30000000-0000-4000-8000-000000000003 的驗收。 (Clawdline 便條 20000000-0000-4000-8000-000000000002：「確認驗收」；待回覆事項：請選擇。)", true, false},
		{"ordinary note mention does not hide foreign item", "請修訂 30000000-0000-4000-8000-000000000003 的驗收，並討論 (Clawdline 便條 20000000-0000-4000-8000-000000000002。", true, false},
		{"unrelated message", "請提醒我明天開會", true, false},
		{"foreign id", "Revise acceptance for 10000000-0000-4000-8000-000000000002", true, false},
		{"prohibition", "不要修改驗收", true, false},
		{"polite prohibition", "請不要修改驗收", true, false},
		{"negative revision stays prohibited", "請不要把驗收修改成不要求額外網址", true, false},
		{"English prohibition stays prohibited", "Please do not revise acceptance for " + id, false, false},
		{"question", "為何不能修改驗收？", true, false},
		{"discussion", "我們應該討論如何修改驗收", true, false},
		{"direct polite question", "可以請你修改這個項目的驗收嗎？", true, true},
		{"policy discussion is not a revision request", "我不認為 Agent 不應該在要求下不能修改驗收", true, false},
		{"explicit prohibition survives two negatives", "我不認為不能討論，但請不要修改驗收", true, false},
		{"request for another action", "請開議題。不要修改驗收", true, false},
		{"unrelated denial before request", "這件事不應由我處理。請修改驗收", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := AcceptanceRevisionInstruction(tc.text, id, "Parent Epic", tc.sole); got != tc.want {
				t.Fatalf("got %t want %t", got, tc.want)
			}
		})
	}
}
