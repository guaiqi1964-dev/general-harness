from __future__ import annotations
import json
import logging
from typing import Any, AsyncIterator, Dict, Optional
from core.plugin_base import BasePlugin, PluginError
from core.types import UnifiedRequest, UnifiedResponse, Usage
logger = logging.getLogger('harness.plugin.openai_compatible')

class OpenAICompatiblePlugin(BasePlugin):
    PLUGIN_NAME = 'openai_compatible'

    def __init__(self, config: Optional[Dict[str, Any]]=None) -> None:
        super().__init__(config)
        self.base_url = (self.config.get('base_url') or '').rstrip('/')
        self.models = list(self.config.get('models') or [])
        self.supports_stream = True

    def validate_config(self) -> None:
        if not self.base_url:
            raise PluginError(f'厂商 {self.name} 缺少 base_url 配置', 500, 'config_error')
        if not any((a['key'] for a in self.keys)):
            raise PluginError(f'厂商 {self.name} 未配置有效的 api_key / api_keys，请在 plugins/{self.name}/config.yaml 中填写（支持 ${{环境变量名}} 写法，避免明文硬编码）', 401, 'authentication_error')
        if not self.models:
            raise PluginError(f'厂商 {self.name} 的 models 列表为空', 500, 'config_error')

    def get_capabilities(self) -> Dict[str, Any]:
        return {'plugin': self.PLUGIN_NAME, 'models': self.models, 'supports_stream': self.supports_stream, 'vision_models': list(self.config.get('vision_models') or []), 'api_keys': [{'name': a['name'], 'configured': bool(a['key'])} for a in self.keys]}

    def _headers(self, api_key: Optional[str]=None) -> Dict[str, str]:
        return {'Authorization': f"Bearer {api_key or self.default_key['key']}", 'Content-Type': 'application/json'}

    def _build_payload(self, request: UnifiedRequest) -> Dict[str, Any]:
        b: Dict[str, Any] = {'model': request.model, 'messages': [a.model_dump() for a in request.messages], 'stream': request.stream}
        if request.temperature is not None:
            b['temperature'] = request.temperature
        if request.max_tokens is not None:
            b['max_tokens'] = request.max_tokens
        if request.top_p is not None:
            b['top_p'] = request.top_p
        a = getattr(request, 'model_extra', None) or {}
        if request.stream and a.get('stream_options') is not None:
            b['stream_options'] = a['stream_options']
        return b

    async def chat_completion(self, request: UnifiedRequest, api_key_selector: Optional[str]=None) -> UnifiedResponse:
        self.validate_config()
        c = self.resolve_key(api_key_selector)
        b = self._build_payload(request)
        d = f'{self.base_url}/chat/completions'
        a = await self._request_json(d, headers=self._headers(c['key']), json=b)
        return self._to_unified_response(a)

    async def stream_chat_completion(self, request: UnifiedRequest, api_key_selector: Optional[str]=None) -> AsyncIterator[UnifiedResponse]:
        self.validate_config()
        e = self.resolve_key(api_key_selector)
        d = self._build_payload(request)
        h = f'{self.base_url}/chat/completions'
        f = ''
        async for c in self._request_stream(h, headers=self._headers(e['key']), json=d):
            c = c.strip()
            if not c or not c.startswith('data:'):
                continue
            b = c[len('data:'):].strip()
            if b == '[DONE]':
                break
            try:
                a = json.loads(b)
            except json.JSONDecodeError:
                logger.warning('[%s] 忽略无法解析的 SSE 行: %s', self.name, c)
                continue
            g = self._chunk_to_unified_response(a)
            if g.id:
                f = f or g.id
            g.id = f
            yield g

    def _to_unified_response(self, data: Dict[str, Any]) -> UnifiedResponse:
        a = (data.get('choices') or [{}])[0]
        b = a.get('message') or {}
        c = data.get('usage')
        return UnifiedResponse(id=data.get('id') or '', content=b.get('content'), usage=self._parse_usage(c), finish_reason=a.get('finish_reason'), model=data.get('model'))

    def _chunk_to_unified_response(self, chunk: Dict[str, Any]) -> UnifiedResponse:
        a = (chunk.get('choices') or [{}])[0]
        b = a.get('delta') or {}
        return UnifiedResponse(id=chunk.get('id') or '', content=b.get('content'), usage=self._parse_usage(chunk.get('usage')), finish_reason=a.get('finish_reason'), model=chunk.get('model'))

    @staticmethod
    def _parse_usage(usage_data: Optional[Dict[str, Any]]) -> Optional[Usage]:
        if not usage_data:
            return None
        return Usage(prompt_tokens=usage_data.get('prompt_tokens', 0), completion_tokens=usage_data.get('completion_tokens', 0), total_tokens=usage_data.get('total_tokens', 0))