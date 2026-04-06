/**
 * Code Printer Module
 * Handles image uploads, estimation requests, and session creation
 */
(function() {
  'use strict';

  // Get CSRF token from app
  async function getCsrfToken() {
    if (window.app && typeof window.app.getCsrfToken === 'function') {
      return await window.app.getCsrfToken();
    }
    return null;
  }

  // State
  const state = {
    images: [], // Array of { file: File, dataUrl: string, base64: string, mediaType: string, name: string }
    estimationResult: null,
    isEstimating: false,
    isBuilding: false  // Prevent double-click on Start Building
  };

  // Constants
  const MAX_IMAGES = 10;  // Maximum number of reference images
  const MAX_IMAGE_SIZE = 10 * 1024 * 1024;  // 10MB per image

  // DOM element references (populated on init)
  let elements = {};

  // Initialize when DOM is ready
  function init() {
    elements = {
      prompt: document.getElementById('codePrinterPrompt'),
      workingDir: document.getElementById('codePrinterWorkingDir'),
      dropzone: document.getElementById('codePrinterDropzone'),
      fileInput: document.getElementById('codePrinterFileInput'),
      previewsContainer: document.getElementById('codePrinterPreviews'),
      previewGrid: document.getElementById('codePrinterPreviewGrid'),
      imageCount: document.getElementById('codePrinterImageCount'),
      estimateBtn: document.getElementById('codePrinterEstimateBtn'),
      estimateStatus: document.getElementById('codePrinterEstimateStatus'),
      results: document.getElementById('codePrinterResults'),
      timeEstimate: document.getElementById('codePrinterTimeEstimate'),
      complexity: document.getElementById('codePrinterComplexity'),
      taskList: document.getElementById('codePrinterTaskList'),
      approach: document.getElementById('codePrinterApproach'),
      timestamp: document.getElementById('codePrinterTimestamp'),
      startBtn: document.getElementById('codePrinterStartBtn')
    };

    // Only setup if elements exist (tab may not be open yet)
    if (elements.dropzone) {
      setupEventListeners();
    }
  }

  function setupEventListeners() {
    // File input change
    if (elements.fileInput) {
      elements.fileInput.addEventListener('change', handleFileSelect);
    }

    // Drag and drop
    if (elements.dropzone) {
      elements.dropzone.addEventListener('dragover', handleDragOver);
      elements.dropzone.addEventListener('dragleave', handleDragLeave);
      elements.dropzone.addEventListener('drop', handleDrop);
      elements.dropzone.addEventListener('click', () => elements.fileInput?.click());
    }
  }

  function handleDragOver(e) {
    e.preventDefault();
    e.stopPropagation();
    elements.dropzone.classList.add('drag-over');
  }

  function handleDragLeave(e) {
    e.preventDefault();
    e.stopPropagation();
    elements.dropzone.classList.remove('drag-over');
  }

  function handleDrop(e) {
    e.preventDefault();
    e.stopPropagation();
    elements.dropzone.classList.remove('drag-over');

    const files = Array.from(e.dataTransfer.files).filter(f => f.type.startsWith('image/'));
    processFiles(files);
  }

  function handleFileSelect(e) {
    const files = Array.from(e.target.files);
    processFiles(files);
    e.target.value = ''; // Reset for re-selection
  }

  async function processFiles(files) {
    // Filter valid files by size
    const validFiles = files.filter(f => f.size <= MAX_IMAGE_SIZE);

    // Check if adding these would exceed the limit
    const remainingSlots = MAX_IMAGES - state.images.length;
    if (remainingSlots <= 0) {
      if (elements.estimateStatus) {
        elements.estimateStatus.textContent = `Maximum ${MAX_IMAGES} images allowed.`;
        elements.estimateStatus.style.color = 'var(--yellow)';
      }
      return;
    }

    // Only process up to remaining slots
    const filesToProcess = validFiles.slice(0, remainingSlots);

    for (const file of filesToProcess) {
      const dataUrl = await readFileAsDataUrl(file);
      const base64 = dataUrl.split(',')[1];
      const mediaType = file.type;

      state.images.push({
        file,
        dataUrl,
        base64,
        mediaType,
        name: file.name
      });
    }

    // Notify if some files were skipped
    if (validFiles.length > filesToProcess.length) {
      if (elements.estimateStatus) {
        elements.estimateStatus.textContent = `Some images skipped. Maximum ${MAX_IMAGES} images allowed.`;
        elements.estimateStatus.style.color = 'var(--yellow)';
      }
    }

    updatePreviewGrid();
  }

  function readFileAsDataUrl(file) {
    return new Promise((resolve, reject) => {
      const reader = new FileReader();
      reader.onload = () => resolve(reader.result);
      reader.onerror = reject;
      reader.readAsDataURL(file);
    });
  }

  function updatePreviewGrid() {
    if (!elements.previewsContainer) return;

    if (state.images.length === 0) {
      elements.previewsContainer.style.display = 'none';
      return;
    }

    elements.previewsContainer.style.display = 'block';
    elements.imageCount.textContent = state.images.length;

    elements.previewGrid.innerHTML = state.images.map((img, idx) => `
      <div class="preview-item" data-index="${idx}">
        <img src="${img.dataUrl}" alt="${img.name}">
        <button class="remove-btn" onclick="codePrinter.removeImage(${idx})" title="Remove">&times;</button>
      </div>
    `).join('');
  }

  function removeImage(index) {
    state.images.splice(index, 1);
    updatePreviewGrid();
  }

  async function estimate() {
    // Re-init elements in case they weren't ready
    if (!elements.prompt) init();

    const prompt = elements.prompt?.value?.trim() || '';

    if (!prompt && state.images.length === 0) {
      if (elements.estimateStatus) {
        elements.estimateStatus.textContent = 'Please provide a description or upload images.';
        elements.estimateStatus.style.color = 'var(--red)';
      }
      return;
    }

    state.isEstimating = true;
    updateEstimateButton(true);

    if (elements.estimateStatus) {
      elements.estimateStatus.textContent = 'Analyzing...';
      elements.estimateStatus.style.color = 'var(--text-dim)';
    }

    if (elements.results) {
      elements.results.style.display = 'none';
    }

    try {
      const csrfToken = await getCsrfToken();
      const headers = { 'Content-Type': 'application/json' };
      if (csrfToken) {
        headers['X-CSRF-Token'] = csrfToken;
      }

      const response = await fetch('/api/code-printer/estimate', {
        method: 'POST',
        headers,
        body: JSON.stringify({
          prompt: prompt,
          images: state.images.map(img => ({
            base64: img.base64,
            mediaType: img.mediaType,
            name: img.name
          })),
          workingDir: elements.workingDir?.value?.trim() || ''
        })
      });

      // Check for network/server errors
      if (!response.ok) {
        throw new Error(`Server error: ${response.status} ${response.statusText}`);
      }

      const result = await response.json();

      if (!result.success) {
        throw new Error(result.error || 'Estimation failed');
      }

      state.estimationResult = result.data;
      displayResults(result.data);

      if (elements.estimateStatus) {
        elements.estimateStatus.textContent = 'Estimation complete.';
        elements.estimateStatus.style.color = 'var(--green)';
      }

    } catch (err) {
      if (elements.estimateStatus) {
        elements.estimateStatus.textContent = `Error: ${err.message}`;
        elements.estimateStatus.style.color = 'var(--red)';
      }
    } finally {
      state.isEstimating = false;
      updateEstimateButton(false);
    }
  }

  function updateEstimateButton(loading) {
    const btn = elements.estimateBtn;
    if (!btn) return;

    const textSpan = btn.querySelector('.btn-text');
    const spinner = btn.querySelector('.btn-spinner');

    btn.disabled = loading;
    if (textSpan) textSpan.textContent = loading ? 'Estimating...' : 'Estimate';
    if (spinner) spinner.style.display = loading ? 'inline-block' : 'none';
  }

  function displayResults(data) {
    if (!elements.results) return;

    // Time estimate
    if (elements.timeEstimate) {
      elements.timeEstimate.textContent = data.timeEstimate || '--';
    }

    // Complexity
    if (elements.complexity) {
      const complexity = data.complexity || 'medium';
      elements.complexity.textContent = complexity.charAt(0).toUpperCase() + complexity.slice(1);
      elements.complexity.className = 'result-value ' + complexity.toLowerCase();
    }

    // Task breakdown
    if (elements.taskList) {
      elements.taskList.innerHTML = (data.tasks || []).map(task => `
        <li>
          <span class="task-name">${escapeHtml(task.name)}</span>
          <span class="task-time">${task.estimatedTime || '--'}</span>
        </li>
      `).join('');
    }

    // Suggested approach
    if (elements.approach) {
      elements.approach.textContent = data.suggestedApproach || 'No specific approach suggested.';
    }

    // Timestamp
    if (elements.timestamp) {
      elements.timestamp.textContent = new Date().toLocaleTimeString();
    }

    // Show results
    elements.results.style.display = 'block';
  }

  function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
  }

  async function startBuilding() {
    if (!state.estimationResult) {
      alert('Please run estimation first.');
      return;
    }

    // Prevent double-click
    if (state.isBuilding) {
      return;
    }

    state.isBuilding = true;
    updateStartButton(true);

    const workingDir = elements.workingDir?.value?.trim() || '';
    const csrfToken = await getCsrfToken();
    const headers = { 'Content-Type': 'application/json' };
    if (csrfToken) {
      headers['X-CSRF-Token'] = csrfToken;
    }

    try {
      // Save images to working directory first so Claude can access them
      let savedImagePaths = [];
      if (state.images.length > 0) {
        const saveRes = await fetch('/api/code-printer/save-images', {
          method: 'POST',
          headers,
          body: JSON.stringify({
            images: state.images.map(img => ({
              base64: img.base64,
              mediaType: img.mediaType,
              name: img.name
            })),
            workingDir: workingDir
          })
        });

        if (!saveRes.ok) {
          throw new Error(`Server error: ${saveRes.status} ${saveRes.statusText}`);
        }

        const saveData = await saveRes.json();
        if (!saveData.success) {
          throw new Error(saveData.error || 'Failed to save images');
        }
        savedImagePaths = saveData.imagePaths || [];
      }

      // Build the full prompt with context and image paths
      const buildPrompt = buildInitialPrompt(savedImagePaths);

      // Create new session
      const createRes = await fetch('/api/sessions', {
        method: 'POST',
        headers,
        body: JSON.stringify({
          workingDir: workingDir,
          name: 'code-printer-' + Date.now()
        })
      });

      if (!createRes.ok) {
        throw new Error(`Server error: ${createRes.status} ${createRes.statusText}`);
      }

      const createData = await createRes.json();
      if (!createData.success) {
        throw new Error(createData.error || 'Failed to create session');
      }

      const sessionId = createData.session.id;

      // Start interactive session (no body needed)
      const interactiveHeaders = csrfToken ? { 'X-CSRF-Token': csrfToken } : {};
      const interactiveRes = await fetch(`/api/sessions/${sessionId}/interactive`, {
        method: 'POST',
        headers: interactiveHeaders
      });

      if (!interactiveRes.ok) {
        throw new Error(`Failed to start interactive session: ${interactiveRes.status}`);
      }

      // Wait a moment for session to initialize
      await new Promise(r => setTimeout(r, 1000));

      // Send the initial prompt
      const inputRes = await fetch(`/api/sessions/${sessionId}/input`, {
        method: 'POST',
        headers,
        body: JSON.stringify({ data: buildPrompt + '\n' })
      });

      if (!inputRes.ok) {
        throw new Error(`Failed to send prompt: ${inputRes.status}`);
      }

      // Switch to the new session
      if (window.app) {
        window.app.closeAppSettings();
        await window.app.selectSession(sessionId);
      }

    } catch (err) {
      alert('Error starting build: ' + err.message);
    } finally {
      state.isBuilding = false;
      updateStartButton(false);
    }
  }

  function updateStartButton(loading) {
    const btn = elements.startBtn;
    if (!btn) return;

    btn.disabled = loading;
    btn.textContent = loading ? 'Starting...' : 'Start Building';
  }

  function buildInitialPrompt(savedImagePaths = []) {
    const userPrompt = elements.prompt?.value?.trim() || '';
    const estimation = state.estimationResult;

    let fullPrompt = userPrompt;

    if (estimation) {
      fullPrompt += `\n\n## Estimation Context\n`;
      fullPrompt += `Estimated Time: ${estimation.timeEstimate}\n`;
      fullPrompt += `Complexity: ${estimation.complexity}\n\n`;
      fullPrompt += `### Task Breakdown:\n`;
      (estimation.tasks || []).forEach((task, i) => {
        fullPrompt += `${i + 1}. ${task.name} (${task.estimatedTime})\n`;
      });
      fullPrompt += `\n### Suggested Approach:\n${estimation.suggestedApproach}\n`;
    }

    // Include saved image paths so Claude can access them
    if (savedImagePaths.length > 0) {
      fullPrompt += `\n\n## Reference Images\n`;
      fullPrompt += `The following reference images have been saved for this project. Please use the Read tool to view them and match the visual design:\n\n`;
      savedImagePaths.forEach((path, i) => {
        fullPrompt += `${i + 1}. ${path}\n`;
      });
      fullPrompt += `\nIMPORTANT: Use the Read tool to view these images before starting implementation.\n`;
    } else if (state.images.length > 0) {
      // Fallback if images weren't saved (shouldn't happen normally)
      fullPrompt += `\n\n## Reference Images\n`;
      fullPrompt += `${state.images.length} screenshot(s) were analyzed for this estimation.\n`;
      fullPrompt += `Please follow the visual design shown in the reference images.\n`;
    }

    return fullPrompt;
  }

  function reset() {
    state.images = [];
    state.estimationResult = null;
    state.isEstimating = false;
    state.isBuilding = false;

    if (elements.prompt) elements.prompt.value = '';
    if (elements.workingDir) elements.workingDir.value = '';
    if (elements.results) elements.results.style.display = 'none';
    if (elements.estimateStatus) elements.estimateStatus.textContent = '';

    updatePreviewGrid();
    updateEstimateButton(false);
    updateStartButton(false);
  }

  // Initialize on DOMContentLoaded
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init);
  } else {
    init();
  }

  // Expose public API
  window.codePrinter = {
    estimate,
    startBuilding,
    removeImage,
    reset,
    init  // Allow re-initialization when tab is opened
  };

})();
