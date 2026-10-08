import os
import sys
import tempfile
import unittest

import anki.collection
from anki.scheduler import CustomStudyRequest

def execute_custom_study_new_cards(col, deck_id=None, extra_count=999):
    """
    针对当前牌组纯粹提升今日新卡片上限（new_limit_delta），
    绝不触碰复习卡（不增加复习上限、不提前复习）。
    返回 (bool success, str message, int new_cards_count)
    """
    if not col or not col.sched:
        return False, "Collection 未就绪", 0
    if deck_id is None:
        deck_id = col.decks.get_current_id()
    if not deck_id:
        return False, "未获取到有效 deck_id", 0

    # 严格仅提升今日新卡片上限
    try:
        req_new = CustomStudyRequest(deck_id=deck_id)
        req_new.new_limit_delta = extra_count
        col.sched.custom_study(req_new)
    except Exception as e:
        return False, f"提升新卡上限异常: {e}", 0

    # 检查新卡数量
    counts = col.sched.counts() # (new, learn, review)
    new_count = counts[0]
    if new_count > 0:
        return True, f"成功提升今日新卡上限 +{extra_count}，当前可用新卡: {new_count}", new_count
    else:
        return False, "牌组中已无更多未学新卡", 0


class TestAutoCustomStudyNewCardsOnly(unittest.TestCase):
    def setUp(self):
        self.tmpdir = tempfile.mkdtemp()
        self.col_path = os.path.join(self.tmpdir, "test_collection.anki2")
        self.col = anki.collection.Collection(self.col_path)
        self.deck_id = self.col.decks.get_current_id()

    def tearDown(self):
        self.col.close()

    def test_increase_new_cards_limit_success(self):
        """测试用例1：今日新卡学完（限额为0），调用函数仅增加新卡上限，完成状态解除"""
        conf = self.col.decks.config_dict_for_deck_id(self.deck_id)
        conf["new"]["perDay"] = 0
        conf["rev"]["perDay"] = 0
        self.col.decks.update_config(conf)

        # 添加一张新卡
        note = self.col.new_note(self.col.models.by_name("Basic"))
        note["Front"] = "New Question 1"
        note["Back"] = "New Answer 1"
        self.col.add_note(note, self.deck_id)

        # 初始由于限额为 0，counts 为 (0, 0, 0)，处于 finished 状态
        self.assertEqual(self.col.sched.counts(), (0, 0, 0))
        self.assertTrue(self.col.sched._is_finished())

        # 执行只提升新卡上限
        success, msg, new_count = execute_custom_study_new_cards(self.col, self.deck_id, extra_count=999)

        # 验证结果：新卡数变为 1，完成状态解除
        self.assertTrue(success)
        self.assertEqual(new_count, 1)
        self.assertEqual(self.col.sched.counts()[0], 1)
        self.assertFalse(self.col.sched._is_finished())

    def test_never_touches_review_cards_limit(self):
        """测试用例2：验证调度器层面严格只修改 extend_new，绝对不修改 extend_review"""
        defaults_before = self.col.sched.custom_study_defaults(self.deck_id)
        self.assertEqual(defaults_before.extend_review, 0)

        # 执行新卡提升函数
        execute_custom_study_new_cards(self.col, self.deck_id, extra_count=999)

        # 验证调度器实际扩展限额：extend_new 增加到 999，而 extend_review 始终为 0
        defaults_after = self.col.sched.custom_study_defaults(self.deck_id)
        self.assertEqual(defaults_after.extend_new, 999)
        self.assertEqual(defaults_after.extend_review, 0)

    def test_when_no_new_cards_available(self):
        """测试用例3：牌组完全没有未学新卡时安全返回 False，不发生死循环和异常"""
        success, msg, new_count = execute_custom_study_new_cards(self.col, self.deck_id, extra_count=999)
        self.assertFalse(success)
        self.assertEqual(new_count, 0)


if __name__ == "__main__":
    unittest.main()
