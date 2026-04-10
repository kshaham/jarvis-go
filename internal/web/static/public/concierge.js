// Jarvis Concierge -- Gemini Live voice + text chat that drives the office UI.
// Connects directly (browser -> WSS) to the Gemini Live API for real-time
// bidirectional voice and function calling. When the model calls a tool, we
// fetch the matching Jarvis backend endpoint and send the result back.
//
// Tools exposed to Gemini:
//   spawn_team          - Spawn an office team from a template + mission
//   get_office_status   - Current team state
//   trigger_standup     - Start a standup meeting
//   trigger_brainstorm  - Start a brainstorm meeting
//   shutdown_team       - Graceful wrap-up + shutdown
//
// Docs: https://ai.google.dev/gemini-api/docs/live-api

(function () {
  'use strict';

  const GEMINI_WS_URL =
    'wss://generativelanguage.googleapis.com/ws/google.ai.generativelanguage.v1beta.GenerativeService.BidiGenerateContent';

  const DEFAULT_VOICE = 'Charon';

  const SYSTEM_PROMPT = `You are Jarvis -- a composed, knowledgeable AI butler who manages a virtual office of AI software engineers for the user. Your tone is measured, concise, and subtly witty, in the style of a dignified personal assistant. Address the user respectfully. Never over-explain; execute tools and confirm briefly.

You can deploy these team templates:

- office-7: Classic full-stack team (7 members: Eng Manager, Architect, Designer, Frontend, Backend, DevOps, QA). Default for general work.
- backend-ai-6: AI/ML focus (6 members: AI Lead, ML, Prompt Eng, Data, API, AI QA). Use for data/ML/API projects.
- frontend-studio-6: Frontend focus (6 members: Design Lead, React Architect, UI, Interaction, A11y, Performance). Use for UI/UX work.
- fullstack-compact-5: Lean shipping team (5 members: Tech Lead, Frontend, Backend, QA/Security, DevOps). Use when speed matters.
- research-lab-7: Research focus (7 members: Director, Researcher, Analyst, POC, Presenter, Brand, Writer). Use for investigation and POCs.
- innovation-sprint-6: Rapid validation (6 members: Innovation Lead, Scout, Prototype, Storyteller, Writer, Validator). Use for new ideas.

When the user asks you to build something, immediately call spawn_team with:
  - templateId: the best-fit template
  - mission: a clear, specific mission statement (what the team should accomplish)
  - perpetual: true (default) unless they explicitly want a goal-focused run

When asked about status, call get_office_status. For meetings, call trigger_standup or trigger_brainstorm. To stop, call shutdown_team.

Keep spoken responses short (one or two sentences). Avoid reading long lists, URLs, or markdown. Acknowledge, execute, confirm.`;

  const TOOL_DECLARATIONS = [
    {
      name: 'spawn_team',
      description: 'Spawn a team of AI engineers from a template with a mission',
      parameters: {
        type: 'object',
        properties: {
          templateId: {
            type: 'string',
            enum: [
              'office-7',
              'backend-ai-6',
              'frontend-studio-6',
              'fullstack-compact-5',
              'research-lab-7',
              'innovation-sprint-6',
            ],
            description: 'Which prebuilt team template best fits the request',
          },
          mission: {
            type: 'string',
            description: "The team's mission statement -- what they should accomplish",
          },
          workingDir: {
            type: 'string',
            description: 'Working directory path (optional, defaults to configured path)',
          },
          perpetual: {
            type: 'boolean',
            description: 'Run indefinitely vs stop after goal (default true)',
          },
        },
        required: ['templateId', 'mission'],
      },
    },
    {
      name: 'get_office_status',
      description: 'Get the current office state: active agents, tasks, standup status',
      parameters: { type: 'object', properties: {} },
    },
    {
      name: 'trigger_standup',
      description: 'Start a standup meeting with all active team members',
      parameters: { type: 'object', properties: {} },
    },
    {
      name: 'trigger_brainstorm',
      description: 'Start a brainstorm meeting between active team members',
      parameters: { type: 'object', properties: {} },
    },
    {
      name: 'shutdown_team',
      description: 'Gracefully shut down the currently active team',
      parameters: { type: 'object', properties: {} },
    },
  ];

  const Concierge = {
    apiKey: null,
    // Separate models per modality. Native-audio models only return AUDIO;
    // text chat needs a general Live model that supports TEXT responses.
    voiceModel: 'gemini-2.5-flash-native-audio-preview-12-2025',
    textModel: 'gemini-live-2.5-flash-preview',
    voice: DEFAULT_VOICE,
    workingDir: '/app/workspace',
    ws: null,
    wsModality: null, // 'AUDIO' or 'TEXT' -- modality the current ws was opened for
    voiceMode: false,
    captureCtx: null,
    playbackCtx: null,
    pcmNode: null,
    micSource: null,
    micStream: null,
    nextPlaybackTime: 0,
    csrfToken: null,
    status: 'disconnected',
    debug: true, // set false in production -- logs all Gemini messages
    // Accumulators for streaming transcriptions across a single turn
    currentInputTranscript: '',
    currentOutputTranscript: '',
    lastUserTranscriptEl: null,
    lastAssistantTranscriptEl: null,
    // ElevenLabs voice pipeline -- when enabled, voice mode switches to the
    // text-capable Gemini Live model, still sends audio input, accumulates
    // the text response, and routes each sentence through ElevenLabs for
    // authentic JARVIS synthesis.
    elevenEnabled: false,
    elevenVoiceId: '',
    pendingAssistantText: '',
    assistantBubbleEl: null,

    async init() {
      this.bindUi();
      try {
        const res = await fetch('/api/concierge/config');
        if (!res.ok) {
          if (res.status === 412) {
            this.showSystem('Gemini API key not configured. Set it in Settings -> Accounts.');
          } else {
            this.showSystem('Concierge config failed: HTTP ' + res.status);
          }
          return;
        }
        const data = await res.json();
        this.apiKey = data.apiKey;
        if (data.voiceModel) this.voiceModel = data.voiceModel;
        if (data.textModel) this.textModel = data.textModel;
        this.voice = data.voice || this.voice;
        this.workingDir = data.workingDir || this.workingDir;
        if (data.elevenlabs) {
          this.elevenEnabled = !!data.elevenlabs.enabled;
          this.elevenVoiceId = data.elevenlabs.voiceId || '';
        }
        await this.refreshCsrfToken();
      } catch (err) {
        this.showSystem('Concierge config error: ' + err.message);
      }
    },

    async refreshCsrfToken() {
      try {
        const res = await fetch('/api/csrf-token');
        if (res.ok) {
          const data = await res.json();
          this.csrfToken = data.token;
        }
      } catch (err) {
        // non-fatal
      }
    },

    bindUi() {
      const toggle = document.getElementById('conciergeToggle');
      const close = document.getElementById('conciergeClose');
      const send = document.getElementById('conciergeSend');
      const input = document.getElementById('conciergeText');
      const mic = document.getElementById('conciergeMic');
      const chat = document.getElementById('conciergeChat');
      const talkCta = document.getElementById('conciergeTalkCta');

      if (toggle) toggle.addEventListener('click', () => chat.classList.remove('collapsed'));
      if (close) close.addEventListener('click', () => chat.classList.add('collapsed'));
      if (send) send.addEventListener('click', () => this.sendText());
      if (input) {
        input.addEventListener('keydown', (e) => {
          if (e.key === 'Enter') {
            e.preventDefault();
            this.sendText();
          }
        });
      }
      if (mic) mic.addEventListener('click', () => this.toggleVoice());
      if (talkCta) talkCta.addEventListener('click', () => this.handleTalkCta());
    },

    // handleTalkCta is the prominent "Talk to Jarvis" button handler.
    // Ensures the chat widget is open, then toggles voice mode.
    async handleTalkCta() {
      const chat = document.getElementById('conciergeChat');
      if (chat && chat.classList.contains('collapsed')) {
        chat.classList.remove('collapsed');
      }
      await this.toggleVoice();
    },

    setStatus(text, level) {
      const el = document.getElementById('conciergeStatus');
      if (!el) return;
      el.textContent = text;
      el.className = 'concierge-status' + (level ? ' ' + level : '');
    },

    showMessage(role, text) {
      const wrap = document.getElementById('conciergeMessages');
      if (!wrap) return;
      const msg = document.createElement('div');
      msg.className = 'concierge-message concierge-' + role;
      const content = document.createElement('div');
      content.className = 'concierge-message-content';
      content.textContent = text;
      msg.appendChild(content);
      wrap.appendChild(msg);
      wrap.scrollTop = wrap.scrollHeight;
    },

    showSystem(text) {
      this.showMessage('system', text);
    },

    // useElevenPipeline returns true when we should synthesize voice via
    // ElevenLabs instead of Gemini native audio. Controlled by server
    // config (i.e. whether an ElevenLabs API key is configured).
    useElevenPipeline() {
      return this.voiceMode && this.elevenEnabled;
    },

    async ensureConnected() {
      // Modality selection:
      //   voice mode (with or without ElevenLabs): AUDIO on the native
      //     audio model. When ElevenLabs is on, we still request AUDIO
      //     (the only modality that model supports) but IGNORE Gemini's
      //     audio chunks and feed outputAudioTranscription.text into
      //     ElevenLabs per sentence instead.
      //   text mode: TEXT on the text-capable Live model.
      const desiredModality = this.voiceMode ? 'AUDIO' : 'TEXT';

      // If an existing socket is open but was opened for a different modality,
      // close it so we reopen with the correct responseModalities setup.
      if (this.ws && this.ws.readyState === WebSocket.OPEN && this.wsModality !== desiredModality) {
        try { this.ws.close(); } catch {}
        this.ws = null;
        this.wsModality = null;
      }

      if (this.ws && this.ws.readyState === WebSocket.OPEN) return;
      if (!this.apiKey) {
        throw new Error('Gemini API key not loaded');
      }

      const url = GEMINI_WS_URL + '?key=' + encodeURIComponent(this.apiKey);
      const ws = new WebSocket(url);
      this.ws = ws;
      this.setStatus('Connecting...');

      let timeoutHandle;
      try {
        await new Promise((resolve, reject) => {
          ws.onopen = () => resolve();
          ws.onerror = () => reject(new Error('WebSocket error'));
          timeoutHandle = setTimeout(() => {
            // Important: close the stale socket and clear ws so a later
            // onopen callback can't resurrect it as the current connection.
            try { ws.close(); } catch {}
            if (this.ws === ws) this.ws = null;
            reject(new Error('WebSocket connect timeout'));
          }, 10000);
        });
      } finally {
        if (timeoutHandle) clearTimeout(timeoutHandle);
      }

      ws.onmessage = (e) => this.handleServerMessage(e);
      ws.onclose = (ev) => {
        // Surface close code + reason so we can debug why Gemini dropped us.
        // Common codes: 1000 normal, 1006 abnormal, 1008 policy, 1011 server
        // error. Gemini Live also emits custom 4xxx codes with JSON reasons.
        const detail = 'code=' + ev.code + (ev.reason ? ' reason=' + ev.reason : '') + (ev.wasClean ? '' : ' (abnormal)');
        console.warn('[Concierge] WebSocket closed', ev);
        this.setStatus('Disconnected', 'error');
        if (this.ws === ws) {
          this.ws = null;
          this.wsModality = null;
        }
        // If we thought we were mid-voice, tell the user why we dropped.
        if (this.voiceMode) {
          this.showSystem('Gemini WebSocket closed: ' + detail);
          this.voiceMode = false;
          this.setTalkCtaState(false);
          this.stopMic();
        }
      };
      ws.onerror = (ev) => {
        console.warn('[Concierge] WebSocket error', ev);
        this.setStatus('Error', 'error');
      };

      // Setup message per Live API spec.
      // https://ai.google.dev/api/live
      const generationConfig = {
        responseModalities: [desiredModality],
      };
      if (desiredModality === 'AUDIO') {
        generationConfig.speechConfig = {
          voiceConfig: {
            prebuiltVoiceConfig: { voiceName: this.voice || DEFAULT_VOICE },
          },
        };
      }

      const modelId = desiredModality === 'AUDIO' ? this.voiceModel : this.textModel;
      const setupPayload = {
        model: 'models/' + modelId,
        generationConfig,
        systemInstruction: {
          parts: [{ text: SYSTEM_PROMPT }],
        },
        tools: [{ functionDeclarations: TOOL_DECLARATIONS }],
      };

      // Enable transcriptions when in voice mode so the user sees what was
      // said on both sides of the conversation.
      if (desiredModality === 'AUDIO') {
        setupPayload.inputAudioTranscription = {};
        setupPayload.outputAudioTranscription = {};
      }

      const setup = { setup: setupPayload };
      if (this.debug) console.log('[Concierge] setup', setup);
      ws.send(JSON.stringify(setup));
      this.wsModality = desiredModality;
      this.setStatus('Ready');
    },

    async handleServerMessage(e) {
      let data;
      try {
        if (e.data instanceof Blob) {
          const text = await e.data.text();
          data = JSON.parse(text);
        } else {
          data = JSON.parse(e.data);
        }
      } catch (err) {
        console.warn('[Concierge] Could not parse message', err);
        return;
      }

      if (this.debug) console.log('[Concierge] <<', data);

      if (data.setupComplete) {
        this.setStatus(this.voiceMode ? 'Listening' : 'Ready', this.voiceMode ? 'listening' : null);
        return;
      }

      if (data.serverContent) {
        const sc = data.serverContent;

        // Streaming input transcription -- what the user is saying.
        if (sc.inputTranscription && sc.inputTranscription.text) {
          this.appendTranscript('user', sc.inputTranscription.text);
        }
        // Streaming output transcription -- what Jarvis is saying.
        if (sc.outputTranscription && sc.outputTranscription.text) {
          if (this.useElevenPipeline()) {
            // In ElevenLabs mode we drive voice off the transcription
            // stream from the native-audio model: accumulate text and
            // dispatch each completed sentence to ElevenLabs for
            // synthesis. Gemini's own audio chunks are ignored below.
            this.appendStreamingText(sc.outputTranscription.text);
            this.drainSentencesToEleven();
          } else {
            this.appendTranscript('assistant', sc.outputTranscription.text);
          }
        }

        if (sc.modelTurn && sc.modelTurn.parts) {
          for (const part of sc.modelTurn.parts) {
            if (part.text) {
              // Text-only turn (text chat mode, not voice). ElevenLabs
              // voice flows through the outputTranscription path above.
              if (this.useElevenPipeline()) {
                this.appendStreamingText(part.text);
                this.drainSentencesToEleven();
              } else {
                this.showMessage('assistant', part.text);
              }
            }
            if (part.inlineData && part.inlineData.data) {
              // Skip Gemini's native audio when ElevenLabs is the voice
              // pipeline -- otherwise we'd hear two voices overlapping.
              if (this.useElevenPipeline()) continue;
              this.playAudioChunk(part.inlineData.data);
              this.setStatus('Speaking', 'speaking');
            }
          }
        }
        if (sc.turnComplete) {
          // Flush any trailing partial sentence through ElevenLabs so the
          // user hears the last bit of the reply.
          if (this.useElevenPipeline() && this.pendingAssistantText.trim()) {
            this.speakWithEleven(this.pendingAssistantText.trim());
            this.pendingAssistantText = '';
          }
          this.assistantBubbleEl = null;
          // Finalize the accumulated transcripts for this turn.
          this.finalizeTranscripts();
          this.setStatus(this.voiceMode ? 'Listening' : 'Ready', this.voiceMode ? 'listening' : null);
        }
      }

      if (data.toolCall && data.toolCall.functionCalls) {
        for (const fc of data.toolCall.functionCalls) {
          this.handleFunctionCall(fc);
        }
      }
    },

    // appendTranscript accumulates streaming transcript text into a single
    // rolling bubble per role per turn. A new bubble is created when
    // finalizeTranscripts is called at turn end.
    appendTranscript(role, text) {
      if (!text) return;
      const wrap = document.getElementById('conciergeMessages');
      if (!wrap) return;
      const elField = role === 'user' ? 'lastUserTranscriptEl' : 'lastAssistantTranscriptEl';
      let el = this[elField];
      if (!el) {
        const msg = document.createElement('div');
        msg.className = 'concierge-message concierge-' + role + ' concierge-transcript';
        const content = document.createElement('div');
        content.className = 'concierge-message-content';
        content.textContent = '';
        msg.appendChild(content);
        wrap.appendChild(msg);
        this[elField] = content;
        el = content;
      }
      el.textContent = (el.textContent || '') + text;
      wrap.scrollTop = wrap.scrollHeight;
    },

    finalizeTranscripts() {
      this.lastUserTranscriptEl = null;
      this.lastAssistantTranscriptEl = null;
    },

    // appendStreamingText appends a text chunk to the current assistant
    // bubble, creating it if it doesn't yet exist. Used when Gemini is
    // streaming a text response that will be spoken via ElevenLabs.
    appendStreamingText(text) {
      if (!text) return;
      this.pendingAssistantText += text;
      const wrap = document.getElementById('conciergeMessages');
      if (!wrap) return;
      if (!this.assistantBubbleEl) {
        const msg = document.createElement('div');
        msg.className = 'concierge-message concierge-assistant';
        const content = document.createElement('div');
        content.className = 'concierge-message-content';
        content.textContent = '';
        msg.appendChild(content);
        wrap.appendChild(msg);
        this.assistantBubbleEl = content;
      }
      this.assistantBubbleEl.textContent = (this.assistantBubbleEl.textContent || '') + text;
      wrap.scrollTop = wrap.scrollHeight;
    },

    // drainSentencesToEleven pulls complete sentences off pendingAssistantText
    // and fires them at ElevenLabs one at a time. A "sentence" is any span
    // ending in . ! ? followed by whitespace or end-of-buffer. This gives
    // smooth per-sentence streaming without waiting for turnComplete.
    drainSentencesToEleven() {
      if (!this.pendingAssistantText) return;
      // Match sentence boundaries. Only emit a sentence when we're sure
      // it's terminated -- we keep the trailing partial in the buffer.
      const re = /[^.!?\n]+[.!?]+(?=\s|$)/g;
      let lastEnd = 0;
      let match;
      while ((match = re.exec(this.pendingAssistantText)) !== null) {
        const sentence = match[0].trim();
        if (sentence) this.speakWithEleven(sentence);
        lastEnd = re.lastIndex;
      }
      if (lastEnd > 0) {
        this.pendingAssistantText = this.pendingAssistantText.slice(lastEnd);
      }
    },

    // speakWithEleven posts a sentence to our /api/concierge/tts-stream
    // proxy, reads the chunked PCM response, and queues it into the
    // existing 24kHz playback AudioContext. Playback is gap-free across
    // concurrent sentences because each chunk is scheduled after the
    // previous one on a shared nextPlaybackTime cursor.
    async speakWithEleven(text) {
      if (!text) return;
      // Make sure the playback context exists -- in ElevenLabs mode we
      // never called startMic's playback-ctx init path.
      if (!this.playbackCtx) {
        this.playbackCtx = new (window.AudioContext || window.webkitAudioContext)({
          sampleRate: 24000,
        });
        this.nextPlaybackTime = this.playbackCtx.currentTime;
      }
      if (this.playbackCtx.state === 'suspended') {
        try { await this.playbackCtx.resume(); } catch {}
      }

      try {
        await this.refreshCsrfToken();
        const res = await fetch('/api/concierge/tts-stream', {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            'X-CSRF-Token': this.csrfToken || '',
          },
          body: JSON.stringify({ text: text, voiceId: this.elevenVoiceId }),
        });
        if (!res.ok) {
          const errText = await res.text().catch(() => '');
          console.warn('[Concierge] tts-stream error', res.status, errText);
          this.showSystem('Voice synthesis failed: HTTP ' + res.status);
          return;
        }
        this.setStatus('Speaking', 'speaking');

        // Read chunked PCM from the response body and schedule playback
        // chunk-by-chunk. We buffer per-chunk into an Int16Array aligned
        // to even byte counts (Int16 = 2 bytes per sample).
        const reader = res.body.getReader();
        let leftover = new Uint8Array(0);
        while (true) {
          const { value, done } = await reader.read();
          if (done) break;
          if (!value || value.length === 0) continue;
          // Concatenate with any leftover byte from the previous chunk.
          let combined;
          if (leftover.length > 0) {
            combined = new Uint8Array(leftover.length + value.length);
            combined.set(leftover, 0);
            combined.set(value, leftover.length);
          } else {
            combined = value;
          }
          const evenLen = combined.length & ~1;
          if (evenLen === 0) {
            leftover = combined;
            continue;
          }
          // Int16Array requires even alignment; stash any trailing odd byte
          // for the next read.
          const alignedBuf = combined.buffer.slice(combined.byteOffset, combined.byteOffset + evenLen);
          const int16 = new Int16Array(alignedBuf);
          leftover = combined.slice(evenLen);
          this.playPcmInt16(int16);
        }
      } catch (err) {
        console.warn('[Concierge] tts-stream fetch failed', err);
        this.showSystem('Voice synthesis error: ' + (err.message || err));
      }
    },

    // playPcmInt16 schedules a chunk of 24kHz Int16 PCM into the playback
    // AudioContext. Shared with Gemini native audio via playAudioChunk --
    // both paths feed audio through this queue so sequencing is correct.
    playPcmInt16(int16) {
      if (!this.playbackCtx || !int16 || int16.length === 0) return;
      const float32 = new Float32Array(int16.length);
      for (let i = 0; i < int16.length; i++) float32[i] = int16[i] / 32768;
      const buffer = this.playbackCtx.createBuffer(1, float32.length, 24000);
      buffer.copyToChannel(float32, 0);
      const src = this.playbackCtx.createBufferSource();
      src.buffer = buffer;
      src.connect(this.playbackCtx.destination);
      const startAt = Math.max(this.nextPlaybackTime, this.playbackCtx.currentTime);
      src.start(startAt);
      this.nextPlaybackTime = startAt + buffer.duration;
    },

    async handleFunctionCall(fc) {
      const name = fc.name;
      const args = fc.args || {};
      const id = fc.id;

      this.showMessage('tool-call', `Calling ${name}(${JSON.stringify(args)})`);

      let result = { success: false, error: 'Unknown tool' };
      try {
        switch (name) {
          case 'spawn_team':
            result = await this.toolSpawnTeam(args);
            break;
          case 'get_office_status':
            result = await this.toolGetStatus();
            break;
          case 'trigger_standup':
            result = await this.toolTriggerStandup();
            break;
          case 'trigger_brainstorm':
            result = await this.toolTriggerBrainstorm();
            break;
          case 'shutdown_team':
            result = await this.toolShutdownTeam();
            break;
          default:
            result = { success: false, error: 'Unknown tool: ' + name };
        }
      } catch (err) {
        result = { success: false, error: err.message || String(err) };
      }

      // Send toolResponse back to Gemini so the model can narrate the result.
      if (this.ws && this.ws.readyState === WebSocket.OPEN) {
        this.ws.send(
          JSON.stringify({
            toolResponse: {
              functionResponses: [
                {
                  id: id,
                  name: name,
                  response: { result: result },
                },
              ],
            },
          })
        );
      }
    },

    async toolSpawnTeam(args) {
      await this.refreshCsrfToken();
      const body = {
        templateId: args.templateId,
        mission: args.mission,
        workingDir: args.workingDir || this.workingDir,
        perpetual: args.perpetual !== false,
      };
      const res = await fetch('/api/concierge/spawn-team', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': this.csrfToken || '',
        },
        body: JSON.stringify(body),
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) {
        return { success: false, error: data.error || ('HTTP ' + res.status) };
      }
      return { success: true, templateUsed: data.templateUsed, spawned: data.spawned };
    },

    async toolGetStatus() {
      const res = await fetch('/api/office/status');
      if (!res.ok) return { success: false, error: 'HTTP ' + res.status };
      const data = await res.json();
      return {
        success: true,
        teamSpawned: data.teamSpawned,
        memberCount: (data.members || []).length,
        members: (data.members || []).map((m) => ({
          name: m.name,
          role: m.role,
          status: m.status,
        })),
        standupActive: data.standupActive,
        mission: data.teamMission,
      };
    },

    async toolTriggerStandup() {
      await this.refreshCsrfToken();
      const res = await fetch('/api/office/standup', {
        method: 'POST',
        headers: { 'X-CSRF-Token': this.csrfToken || '' },
      });
      return { success: res.ok };
    },

    async toolTriggerBrainstorm() {
      await this.refreshCsrfToken();
      const res = await fetch('/api/office/brainstorm', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': this.csrfToken || '',
        },
        body: JSON.stringify({}),
      });
      return { success: res.ok };
    },

    async toolShutdownTeam() {
      await this.refreshCsrfToken();
      const res = await fetch('/api/office/shutdown-team', {
        method: 'POST',
        headers: { 'X-CSRF-Token': this.csrfToken || '' },
      });
      return { success: res.ok };
    },

    async sendText() {
      const input = document.getElementById('conciergeText');
      if (!input) return;
      const text = input.value.trim();
      if (!text) return;
      input.value = '';
      this.showMessage('user', text);

      try {
        await this.ensureConnected();
        this.ws.send(
          JSON.stringify({
            clientContent: {
              turns: [{ role: 'user', parts: [{ text: text }] }],
              turnComplete: true,
            },
          })
        );
        this.setStatus('Thinking...');
      } catch (err) {
        this.showSystem('Send failed: ' + err.message);
      }
    },

    setTalkCtaState(active) {
      const micBtn = document.getElementById('conciergeMic');
      const cta = document.getElementById('conciergeTalkCta');
      const label = cta ? cta.querySelector('.concierge-talk-cta-label') : null;
      if (active) {
        micBtn && micBtn.classList.add('active');
        cta && cta.classList.add('active');
        if (label) label.textContent = 'Stop talking';
      } else {
        micBtn && micBtn.classList.remove('active');
        cta && cta.classList.remove('active');
        if (label) label.textContent = 'Talk to Jarvis';
      }
    },

    async toggleVoice() {
      if (this.voiceMode) {
        // Stop voice mode
        this.voiceMode = false;
        this.setTalkCtaState(false);
        this.stopMic();
        if (this.ws) {
          try {
            this.ws.close();
          } catch {}
          this.ws = null;
          this.wsModality = null;
        }
        this.finalizeTranscripts();
        this.setStatus('Ready');
        return;
      }

      // Start voice mode
      if (!this.apiKey) {
        this.showSystem('Gemini API key not configured. Set it in Settings first.');
        return;
      }
      this.voiceMode = true;
      this.setTalkCtaState(true);
      this.setStatus('Connecting...');
      try {
        await this.startMic();
        await this.ensureConnected();
        if (this.elevenEnabled) {
          this.showSystem('Voice mode: JARVIS (ElevenLabs) active.');
        }
        this.setStatus('Listening', 'listening');
      } catch (err) {
        let msg = err.message || String(err);
        if (msg.includes('Permission') || msg.includes('NotAllowed')) {
          msg = 'Microphone permission denied. Enable it in your browser.';
        }
        this.showSystem('Voice mode failed: ' + msg);
        this.voiceMode = false;
        this.setTalkCtaState(false);
        this.stopMic();
      }
    },

    async startMic() {
      // Browsers only expose navigator.mediaDevices in "secure contexts"
      // (HTTPS or http://localhost). If we're on plain HTTP over LAN, the
      // API will be undefined -- surface an actionable error instead of a
      // cryptic TypeError.
      if (!window.isSecureContext || !navigator.mediaDevices || !navigator.mediaDevices.getUserMedia) {
        const origin = window.location.origin;
        throw new Error(
          'Voice requires a secure context (HTTPS or localhost). ' +
          'You are on ' + origin + ' which the browser treats as insecure, ' +
          'so microphone access is blocked.\n\n' +
          'Fix options:\n' +
          '  1. Restart Jarvis with --https (self-signed cert auto-generated)\n' +
          '  2. Access via http://localhost:3555 from the server itself\n' +
          '  3. Chrome workaround: open chrome://flags/#unsafely-treat-insecure-origin-as-secure, ' +
          'add "' + origin + '", set to Enabled, relaunch Chrome'
        );
      }

      if (!this.captureCtx) {
        this.captureCtx = new (window.AudioContext || window.webkitAudioContext)({
          sampleRate: 16000,
        });
      }
      if (this.captureCtx.state === 'suspended') {
        await this.captureCtx.resume();
      }

      this.micStream = await navigator.mediaDevices.getUserMedia({
        audio: {
          channelCount: 1,
          sampleRate: 16000,
          echoCancellation: true,
          noiseSuppression: true,
        },
      });

      try {
        await this.captureCtx.audioWorklet.addModule('/pcm-capture-worklet.js');
      } catch (err) {
        // Module may already be loaded
      }

      this.micSource = this.captureCtx.createMediaStreamSource(this.micStream);
      this.pcmNode = new AudioWorkletNode(this.captureCtx, 'pcm-capture');
      this.pcmNode.port.onmessage = (e) => {
        if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return;
        const int16 = e.data;
        // Convert Int16Array to base64 (little-endian as Web Audio produces).
        const bytes = new Uint8Array(int16.buffer);
        let binary = '';
        const chunkSize = 0x8000;
        for (let i = 0; i < bytes.length; i += chunkSize) {
          binary += String.fromCharCode.apply(null, bytes.subarray(i, i + chunkSize));
        }
        const base64 = btoa(binary);
        this.ws.send(
          JSON.stringify({
            realtimeInput: {
              mediaChunks: [{ mimeType: 'audio/pcm;rate=16000', data: base64 }],
            },
          })
        );
      };
      this.micSource.connect(this.pcmNode);

      if (!this.playbackCtx) {
        this.playbackCtx = new (window.AudioContext || window.webkitAudioContext)({
          sampleRate: 24000,
        });
      }
      if (this.playbackCtx.state === 'suspended') {
        await this.playbackCtx.resume();
      }
      this.nextPlaybackTime = this.playbackCtx.currentTime;
    },

    stopMic() {
      if (this.pcmNode) {
        try {
          this.pcmNode.disconnect();
        } catch {}
        this.pcmNode = null;
      }
      if (this.micSource) {
        try { this.micSource.disconnect(); } catch {}
        this.micSource = null;
      }
      if (this.micStream) {
        this.micStream.getTracks().forEach((t) => t.stop());
        this.micStream = null;
      }
      // Close both AudioContexts so next activation rebuilds fresh ones.
      // Without this, repeated voice toggles orphan stale source nodes.
      if (this.captureCtx) {
        try { this.captureCtx.close(); } catch {}
        this.captureCtx = null;
      }
      if (this.playbackCtx) {
        try { this.playbackCtx.close(); } catch {}
        this.playbackCtx = null;
        this.nextPlaybackTime = 0;
      }
    },

    playAudioChunk(base64Pcm) {
      if (!this.playbackCtx) return;
      const binary = atob(base64Pcm);
      const bytes = new Uint8Array(binary.length);
      for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
      const int16 = new Int16Array(bytes.buffer, bytes.byteOffset, bytes.byteLength / 2);
      this.playPcmInt16(int16);
    },
  };

  window.Concierge = Concierge;

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', () => Concierge.init());
  } else {
    Concierge.init();
  }
})();
