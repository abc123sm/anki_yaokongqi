from aqt import mw
from aqt.qt import *
from aqt.utils import showInfo, tooltip
import json
import requests
import os
import time

DEFAULT_CONFIG = {
  "auth_token": "Bearer abc123sm",
  "apiurl": "https://anki-ai-proxy.abc123sm.workers.dev",
  "model": "@cf/meta/llama-3.3-70b-instruct-fp8-fast",
  "max_context": 6,
  "temperature": 0.7,
  "top_p": 1.0,
  "max_tokens": 1000,
  "system_prompt": "你是一个专业的日语语言助手，负责处理日语句子的标注和解析。严格遵循以下规则：\n\n1. 处理规则：\n- 对所有汉字添加正确的假名标注，使用[ ]包裹\n- 保留原始句子格式和分段\n\n2. 标注原则：\n- 汉字必须标注对应的读音假名\n- 标注时不改变原句结构\n- 保留原文中的数字、标点和其他特殊符号\n- 仅对汉字进行标注，不对平假名或片假名进行标注\n\n3. 解析要求：\n- 将句子拆分为3至8个语法单元\n- 每个单元包含：\n  * 原文（有汉字则标注[ ]）\n  * 简洁的语义解释（避免使用语法术语）\n\n4. 输出格式：\n- 拆分单元：语义解释\n- 整体中文翻译\n\n5. 输出要求： \n- 只使用文本回复，不使用markdown语法\n- 只进行单个换行\n- 不分段\n- 假名不需要再用假名标注\n- 不要在输出中包含原句或格式要求的说明\n\n6. 输出示例：\nその：表示指示，\"那个\"\n度[たび]：表示次数或事件，\"次\"\n１人[ひとり] で：表示独自，\"一个人\"\n何[なん]とか して きた：表示尽力做，\"想办法做了\"\nん だ：表示解释或强调，\"呢\"\n每次都是一个人尽力去做的。",
  "question_field": "Expression",
  "answer_field": "Edit",
  "context_messages": [
    { "role": "user", "content": "なんと！　与えたほうまで元気に？" },
    { "role": "assistant", "content": "なんと！：表示惊讶，\"竟然！\"\n与[あた]えたほうまで：表示连同给予者，\"连给的那一方也\"\n元気[げんき]に？：表示疑问，\"精神起来了？\"\n竟然！连给予者都精神起来了？" },
    { "role": "user", "content": "分かった １日に持ってくで" },
    { "role": "assistant", "content": "分[わ]かった：表示理解，\"明白了\"\n１日[ついたち]に：表示日期，\"在1号\"\n持[も]ってくで：表示动作和意愿，\"会拿过去/带来\"\n明白了，1号会拿过去。" },
    { "role": "user", "content": "何が ‎みんなで協力すれば何とかなるだ" },
    { "role": "assistant", "content": "何[なに]が：表示疑问或反驳，\"什么\"\nみんなで：表示共同行动，\"大家一起\"\n協力[きょうりょく]すれば：表示条件，\"如果合作的话\"\n何[なん]とかなるだ：表示可以解决，\"就能解决什么的\"\n什么大家一起合作就能解决啊。" },
    { "role": "user", "content": "外からは　見えないようにしておるのじゃが" },
    { "role": "assistant", "content": "外[そと]からは：表示来源或视角，\"从外面\"\n見[み]えないように：表示目的或状态，\"为了不被看到\"\nしておるのじゃが：表示持续的状态或情况，带有强调，\"正在做着呢\"\n从外面看是看不到的。" },
    { "role": "user", "content": "来た！　腹を決めやがった" },
    { "role": "assistant", "content": "来[き]た！：表示出现，\"来了！\"\n腹[はら]を：指决心或意志，\"心意\"\n決[き]めやがった：表示下决心（带有粗鲁或不屑的语气），\"下定了\"\n来了！他下定决心了。" },
    { "role": "user", "content": "負けるのが分かってる　皿を出すのが怖いんだ" },
    { "role": "assistant", "content": "負[ま]けるのが：表示失败，\"会输\"\n分[わ]かってる：表示已知晓，\"知道了\"\n皿[さら]を：指要下的棋子或牌，\"盘子（棋子）\"\n出[だ]すのが：表示拿出或下，\"拿出来\"\n怖[こわ]いんだ：表示心情，\"很害怕\"\n知道会输，所以不敢下棋（出牌）。" }
  ]
}


def get_config():
    """使用 Anki API 加载配置，如果不存在则自动创建"""
    config = mw.addonManager.getConfig(__name__)
    if config is None:
        # 第一次运行，写入并返回默认配置
        mw.addonManager.writeConfig(__name__, DEFAULT_CONFIG)
        return DEFAULT_CONFIG
    
    # 检查是否有在更新后新增的配置项，并合并
    config_updated = False
    for key, value in DEFAULT_CONFIG.items():
        if key not in config:
            config[key] = value
            config_updated = True
    
    if config_updated:
        mw.addonManager.writeConfig(__name__, config)
        
    return config

def save_config(config):
    """使用 Anki API 保存配置"""
    try:
        mw.addonManager.writeConfig(__name__, config)
        return True
    except Exception as e:
        showInfo(f"保存配置失败: {str(e)}")
        return False


class ConfigDialog(QDialog):
    def __init__(self, parent=None):
        super().__init__(parent)
        self.setWindowTitle("CF AI提问配置")
        self.setMinimumWidth(700)
        self.setMinimumHeight(800)
        self.layout = QVBoxLayout()
        self.setLayout(self.layout)
        self.setup_ui()
        
    def setup_ui(self):
        config = get_config()
        if not config:
            return
            
        # 基础设置
        form_layout = QFormLayout()

        
        self.auth_token = QLineEdit(config.get("auth_token", ""))
        form_layout.addRow("Authorization Token:", self.auth_token)

        
        self.apiurl = QLineEdit(config["apiurl"])
        form_layout.addRow("API URL:", self.apiurl)
        
        self.model = QLineEdit(config["model"])
        form_layout.addRow("模型:", self.model)
        
        self.temperature = QDoubleSpinBox()
        self.temperature.setRange(0.0, 5.0)
        self.temperature.setSingleStep(0.1)
        self.temperature.setDecimals(1)
        self.temperature.setValue(config["temperature"])
        form_layout.addRow("温度:", self.temperature)

        self.top_p = QDoubleSpinBox()
        self.top_p.setRange(0.001, 1.0)
        self.top_p.setSingleStep(0.01)
        self.top_p.setDecimals(3)
        self.top_p.setValue(config["top_p"])
        form_layout.addRow("Top P:", self.top_p)

        self.max_tokens = QSpinBox()
        self.max_tokens.setRange(1, 4096)
        self.max_tokens.setValue(config["max_tokens"])
        form_layout.addRow("最大Token数:", self.max_tokens)
        
        self.max_context = QSpinBox()
        self.max_context.setRange(1, 20)
        self.max_context.setValue(config["max_context"])
        form_layout.addRow("上下文消息数量上限:", self.max_context)
        
        
        self.question_field = QLineEdit(config["question_field"])
        form_layout.addRow("提问字段:", self.question_field)
        
        self.answer_field = QLineEdit(config["answer_field"])
        form_layout.addRow("回答字段:", self.answer_field)
        
        self.layout.addLayout(form_layout)
        
        # 系统提示词
        self.layout.addWidget(QLabel("系统提示词:"))
        self.system_prompt = QTextEdit()
        self.system_prompt.setPlainText(config["system_prompt"])
        self.system_prompt.setMaximumHeight(200)
        self.layout.addWidget(self.system_prompt)
        
        # 上下文消息
        self.layout.addWidget(QLabel("上下文消息:"))
        self.context_scroll = QScrollArea()
        self.context_widget = QWidget()
        self.context_layout = QVBoxLayout(self.context_widget)
        self.context_edits = []
        
        # 创建上下文编辑框
        context_messages = config["context_messages"]
        for i in range(0, len(context_messages), 2):
            if i + 1 >= len(context_messages):
                break
                
            group_widget = QGroupBox(f"对话 {i//2 + 1}")
            group_layout = QVBoxLayout()
            
            # 用户消息
            user_layout = QHBoxLayout()
            user_layout.addWidget(QLabel("用户:"))
            user_edit = QTextEdit()
            user_edit.setPlainText(context_messages[i]["content"])
            user_edit.setMaximumHeight(80)
            user_layout.addWidget(user_edit)
            group_layout.addLayout(user_layout)
            
            # 助手消息
            assistant_layout = QHBoxLayout()
            assistant_layout.addWidget(QLabel("助手:"))
            assistant_edit = QTextEdit()
            assistant_edit.setPlainText(context_messages[i+1]["content"])
            assistant_edit.setMaximumHeight(80)
            assistant_layout.addWidget(assistant_edit)
            group_layout.addLayout(assistant_layout)
            
            group_widget.setLayout(group_layout)
            self.context_layout.addWidget(group_widget)
            self.context_edits.append((user_edit, assistant_edit))
        
        self.context_scroll.setWidget(self.context_widget)
        self.context_scroll.setWidgetResizable(True)
        self.context_scroll.setMaximumHeight(400)
        self.layout.addWidget(self.context_scroll)
        
        # 按钮
        button_layout = QHBoxLayout()
        save_btn = QPushButton("保存")
        save_btn.clicked.connect(self.save_config)
        button_layout.addWidget(save_btn)
        
        cancel_btn = QPushButton("取消")
        cancel_btn.clicked.connect(self.reject)
        button_layout.addWidget(cancel_btn)
        
        self.layout.addLayout(button_layout)
    
    def save_config(self):
        try:
            config = {
                "auth_token": self.auth_token.text(),
                "apiurl": self.apiurl.text(),
                "model": self.model.text(),
                "temperature": self.temperature.value(),
                "top_p": self.top_p.value(),
                "max_tokens": self.max_tokens.value(),
                "max_context": self.max_context.value(),
                "system_prompt": self.system_prompt.toPlainText(),
                "question_field": self.question_field.text(),
                "answer_field": self.answer_field.text(),
                "context_messages": []
            }
            
            for user_edit, assistant_edit in self.context_edits:
                user_content = user_edit.toPlainText().strip()
                assistant_content = assistant_edit.toPlainText().strip()
                
                if user_content:
                    config["context_messages"].append({
                        "role": "user",
                        "content": user_content
                    })
                    config["context_messages"].append({
                        "role": "assistant", 
                        "content": assistant_content
                    })
            
            if save_config(config):
                tooltip("配置保存成功")
                self.accept()
            else:
                showInfo("配置保存失败")
                
        except Exception as e:
            showInfo(f"保存配置时出错: {str(e)}")

def call_chat_api(question):
    """调用 Cloudflare Workers AI / OpenAI 兼容接口"""
    config = get_config()
    if not config:
        return "配置错误"

    try:
        messages = []

        if config.get("system_prompt"):
            messages.append({
                "role": "system",
                "content": config["system_prompt"]
            })

        for msg in config.get("context_messages", []):
            messages.append({
                "role": msg["role"],
                "content": msg["content"]
            })

        messages.append({
            "role": "user",
            "content": question
        })

        data = {
            "model": config["model"],
            "messages": messages,
            "stream": False
        }

        headers = {
            "Content-Type": "application/json"
        }

        auth_token = config.get("auth_token")
        if auth_token:
            headers["Authorization"] = auth_token

        response = requests.post(
            config["apiurl"],
            json=data,
            headers=headers,
            timeout=60
        )
        response.raise_for_status()

        result = response.json()

        # Cloudflare Workers AI
        if isinstance(result, dict):
            cf_resp = result.get("response")
            if isinstance(cf_resp, str):
                return cf_resp.strip()

        # OpenAI / 反代
        choices = result.get("choices")
        if isinstance(choices, list) and choices:
            message = choices[0].get("message")
            if isinstance(message, dict):
                content = message.get("content")
                if isinstance(content, str):
                    return content.strip()

        return "API返回错误：无法解析模型返回"

    except Exception as e:
        return f"API调用错误: {str(e)}"


def generate_ai_response():
    """生成AI回复 (兼容 Edit Field During Review 插件)"""
    try:
        # 确保在复习模式下
        if not mw.reviewer or not mw.reviewer.card:
            showInfo("请在复习模式下使用此功能")
            return
        
        card = mw.reviewer.card
        note = card.note()
        config = get_config()
        if not config:
            return
        
        # 获取问题和回答字段的名称
        question_field = config["question_field"]
        answer_field = config["answer_field"]
        
        # 检查字段是否存在
        if question_field not in note:
            showInfo(f"模板中未找到字段 '{question_field}'，请检查配置")
            return
        if answer_field not in note:
            showInfo(f"模板中未找到字段 '{answer_field}'，请检查配置")
            return
            
        question = note[question_field]
        if not question.strip():
            showInfo(f"字段 '{question_field}' 内容为空")
            return
        
        tooltip("正在向AI提问，请稍候...")
        mw.app.processEvents()

        # 调用API
        response = call_chat_api(question)
        
        # 将换行符 \n 替换为HTML的 <br>
        response_html = response.replace('\r\n', '<br>').replace('\n', '<br>')
        
        if note[answer_field] != response_html:
            note[answer_field] = response_html
            note.flush()
            # 兼容不同版本 Anki 的卡片重载，确保内部数据最新
            if hasattr(mw.col, "update_note"):
                mw.col.update_note(note)
            else:
                card.load()
            # 极度关键：必须清除 Anki 的 HTML 渲染缓存！
            # 否则 AnkiConnect (及 card.a()) 将永远返回生成前的空内容
            card._a = None
            card._q = None
        
        if mw.reviewer.state == "answer":
            escaped_html = json.dumps(response_html)
            
            js_code = f"""
            var field = document.querySelector('[data-field="{answer_field}"]');
            if (field) {{
                field.innerHTML = {escaped_html};
            }}
            """
            mw.reviewer.web.eval(js_code)

        tooltip("AI回复已生成并更新")
        
    except Exception as e:
        showInfo(f"生成AI回复时出错: {str(e)}")




def setup_menu():
    """设置菜单"""
    action = QAction("CF AI提问配置", mw)
    action.triggered.connect(show_config_dialog)
    mw.form.menuTools.addAction(action)

def show_config_dialog():
    """显示配置对话框"""
    try:
        dialog = ConfigDialog(mw)
        dialog.exec()
    except Exception as e:
        showInfo(f"打开配置对话框时出错: {str(e)}")

# 处理来自前端的消息
def handle_webview_message(handled, cmd, context):
    if cmd == "CF_aiGenerate":
        generate_ai_response()
        return (True, None)
    return handled

# 注册钩子
from aqt.gui_hooks import webview_did_receive_js_message
webview_did_receive_js_message.append(handle_webview_message)

# 初始化
setup_menu()

# ----------------- 远程控制扩展 -----------------
import threading
from http.server import BaseHTTPRequestHandler, HTTPServer

class RemoteAIRequestHandler(BaseHTTPRequestHandler):
    def do_OPTIONS(self):
        self.send_response(200, "ok")
        self.send_header('Access-Control-Allow-Origin', '*')
        self.send_header('Access-Control-Allow-Methods', 'POST, OPTIONS')
        self.send_header("Access-Control-Allow-Headers", "X-Requested-With, Content-Type")
        self.end_headers()

    def do_POST(self):
        if self.path == '/trigger-ai':
            # 使用 Event 阻塞等待 AI 在主线程执行完毕
            done_event = threading.Event()
            
            def run_and_notify():
                try:
                    generate_ai_response()
                finally:
                    done_event.set()

            # 在主线程执行 Anki 的 UI/数据操作
            mw.taskman.run_on_main(run_and_notify)
            
            # 等待主线程执行完毕 (最多等待 60 秒，防止卡死)
            done_event.wait(60)

            self.send_response(200)
            self.send_header('Access-Control-Allow-Origin', '*')
            self.send_header('Content-Type', 'application/json')
            self.end_headers()
            self.wfile.write(b'{"result": "success"}')
        else:
            self.send_response(404)
            self.end_headers()

    # 隐藏控制台的请求日志输出
    def log_message(self, format, *args):
        pass

def start_remote_server():
    try:
        server = HTTPServer(('0.0.0.0', 8766), RemoteAIRequestHandler)
        threading.Thread(target=server.serve_forever, daemon=True).start()
    except Exception as e:
        print("启动AI遥控扩展服务失败:", e)

# 启动后台监听服务
start_remote_server()
