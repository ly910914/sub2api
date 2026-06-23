<template>
  <AppLayout>
    <div class="image-page-shell">
      <div v-if="loadingKeys" class="image-state-panel">
        <div class="h-8 w-8 animate-spin rounded-full border-2 border-stone-200 border-b-stone-900 dark:border-stone-700 dark:border-b-stone-100"></div>
      </div>

      <div v-else-if="imageKeys.length === 0" class="image-state-panel border-dashed">
        <div class="max-w-md text-center">
          <div class="mx-auto flex h-12 w-12 items-center justify-center rounded-2xl bg-stone-950 text-white shadow-sm dark:bg-white dark:text-stone-950">
            <Icon name="sparkles" size="lg" />
          </div>
          <h1 class="mt-5 text-xl font-semibold text-stone-950 dark:text-white">{{ t('images.emptyKeysTitle') }}</h1>
          <p class="mt-2 text-sm leading-6 text-stone-500 dark:text-stone-400">{{ t('images.emptyKeysDescription') }}</p>
          <div class="mt-6 flex flex-wrap justify-center gap-2">
            <button type="button" class="image-soft-button" :disabled="loadingKeys" @click="loadKeys">
              <Icon name="refresh" size="sm" :class="loadingKeys ? 'animate-spin' : ''" />
              {{ t('common.refresh') }}
            </button>
            <router-link to="/keys" class="image-primary-button">
              <Icon name="plus" size="sm" />
              {{ t('images.createKey') }}
            </router-link>
          </div>
        </div>
      </div>

      <div v-else class="image-workspace">
        <aside class="image-sidebar hidden lg:flex">
          <div class="flex items-center gap-2">
            <button type="button" class="image-primary-button h-10 flex-1 rounded-xl" @click="startNewConversation">
              <Icon name="chat" size="sm" />
              {{ t('images.newChat') }}
            </button>
            <button
              type="button"
              class="image-icon-button h-10 w-10 rounded-xl"
              :disabled="conversations.length === 0"
              :title="t('images.clearHistory')"
              @click="openClearHistoryConfirm"
            >
              <Icon name="trash" size="sm" />
            </button>
          </div>

          <div class="image-history-list">
            <p v-if="conversations.length === 0" class="px-2 py-3 text-sm leading-6 text-stone-500 dark:text-stone-400">
              {{ t('images.emptyHistory') }}
            </p>
            <div
              v-for="conversation in conversations"
              v-else
              :key="conversation.id"
              class="group relative border-l-2 px-3 py-3 transition-colors"
              :class="conversation.id === activeConversationId ? 'border-stone-950 bg-black/[0.035] text-stone-950 dark:border-white dark:bg-white/10 dark:text-white' : 'border-transparent text-stone-700 hover:border-stone-300 hover:bg-white/50 dark:text-stone-300 dark:hover:border-stone-600 dark:hover:bg-white/5'"
            >
              <button type="button" class="block w-full pr-16 text-left" @click="activeConversationId = conversation.id">
                <input
                  v-if="editingConversationId === conversation.id"
                  v-model="editingConversationTitle"
                  class="w-full truncate rounded-md border border-stone-300 bg-white px-1.5 py-0.5 text-sm font-semibold text-stone-900 outline-none focus:border-stone-500 dark:border-white/10 dark:bg-stone-900 dark:text-white"
                  @click.stop
                  @blur="commitRenameConversation"
                  @keydown.enter.prevent="commitRenameConversation"
                  @keydown.esc.prevent="cancelRenameConversation"
                />
                <span v-else class="block truncate text-sm font-semibold">{{ conversation.title }}</span>
                <span class="mt-1 block truncate text-xs text-stone-400">
                  {{ conversation.turns.length }} · {{ formatTime(conversation.updatedAt) }}
                </span>
                <span v-if="conversationStats(conversation).running > 0 || conversationStats(conversation).queued > 0" class="mt-2 flex flex-wrap gap-1.5 text-[11px]">
                  <span v-if="conversationStats(conversation).running > 0" class="rounded-full bg-blue-50 px-2 py-0.5 text-blue-600 dark:bg-blue-950/40 dark:text-blue-200">{{ t('images.processingCount', { count: conversationStats(conversation).running }) }}</span>
                  <span v-if="conversationStats(conversation).queued > 0" class="rounded-full bg-amber-50 px-2 py-0.5 text-amber-700 dark:bg-amber-950/40 dark:text-amber-200">{{ t('images.queuedCount', { count: conversationStats(conversation).queued }) }}</span>
                </span>
              </button>
              <div class="absolute right-1.5 top-2.5 flex items-center gap-0.5 opacity-100 transition sm:opacity-0 sm:group-hover:opacity-100">
                <button
                  type="button"
                  class="inline-flex h-7 w-7 items-center justify-center rounded-lg text-stone-400 transition hover:bg-stone-100 hover:text-stone-700 dark:hover:bg-white/10 dark:hover:text-white"
                  :title="t('images.renameConversation')"
                  @click="startRenameConversation(conversation, $event)"
                >
                  <Icon name="edit" size="xs" />
                </button>
                <button
                  type="button"
                  class="inline-flex h-7 w-7 items-center justify-center rounded-lg text-stone-400 transition hover:bg-stone-100 hover:text-red-500 dark:hover:bg-white/10"
                  :title="t('common.delete')"
                  @click="openDeleteConversationConfirm(conversation.id)"
                >
                  <Icon name="trash" size="xs" />
                </button>
              </div>
            </div>
          </div>

          <div class="space-y-2 border-t border-stone-200/70 pt-3 dark:border-white/10">
            <button type="button" class="image-soft-button w-full justify-center" :disabled="loadingKeys" @click="loadKeys">
              <Icon name="refresh" size="sm" :class="loadingKeys ? 'animate-spin' : ''" />
              {{ t('common.refresh') }}
            </button>
            <router-link to="/keys" class="image-soft-button w-full justify-center">
              <Icon name="key" size="sm" />
              {{ t('images.manageKeys') }}
            </router-link>
          </div>
        </aside>

        <section class="image-main">
          <div class="flex items-center justify-between gap-2 px-1 lg:hidden">
            <button type="button" class="image-soft-button h-10 flex-1 justify-center" @click="mobileHistoryOpen = true">
              <Icon name="clock" size="sm" />
              {{ t('images.history') }} ({{ conversations.length }})
            </button>
            <button type="button" class="image-primary-button h-10 w-10 rounded-2xl px-0" :title="t('images.newChat')" @click="startNewConversation">
              <Icon name="plus" size="sm" />
            </button>
            <button
              type="button"
              class="image-icon-button h-10 w-10 rounded-2xl"
              :disabled="conversations.length === 0"
              :title="t('images.clearHistory')"
              @click="openClearHistoryConfirm"
            >
              <Icon name="trash" size="sm" />
            </button>
          </div>

          <p v-if="modelLoadError" class="image-content-width rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800 dark:border-amber-900/50 dark:bg-amber-950/30 dark:text-amber-200">
            {{ modelLoadError }}
          </p>

          <div class="relative min-h-0 flex-1">
            <div ref="resultPanelRef" class="image-results-scroll">
              <div v-if="!activeConversation" class="flex h-full min-h-[320px] items-center justify-center px-4 text-center sm:min-h-[440px]">
                <div class="w-full max-w-4xl">
                  <h1 class="image-empty-title">{{ t('images.startTitle') }}</h1>
                  <p class="image-empty-subtitle mx-auto mt-4 max-w-xl text-sm italic leading-6 text-stone-500 dark:text-stone-400">
                    {{ t('images.startDescription') }}
                  </p>
                </div>
              </div>

              <div v-else class="image-content-width flex flex-col gap-6 px-1 py-2 sm:gap-8 sm:px-4 sm:py-4">
                <article v-for="(turn, turnIndex) in activeConversation.turns" :key="turn.id" class="flex flex-col gap-3 sm:gap-4">
                  <div v-if="!turn.promptDeleted" class="flex justify-end">
                    <div class="max-w-[90%] px-1 py-1 text-right text-sm leading-6 text-stone-900 dark:text-stone-100 sm:max-w-[82%] sm:text-[15px] sm:leading-7">
                      <div class="mb-2 flex flex-wrap justify-end gap-2 text-[11px] text-stone-400 dark:text-stone-500">
                        <span>{{ t('images.turnNumber', { count: turnIndex + 1 }) }}</span>
                        <span>{{ turnModeLabel(turn) }}</span>
                        <span>{{ turnStatusLabel(turn) }}</span>
                        <span>{{ formatTime(turn.createdAt) }}</span>
                      </div>
                      <p class="whitespace-pre-wrap">{{ turn.prompt }}</p>
                      <div class="mt-2 flex flex-wrap justify-end gap-1.5">
                        <button type="button" class="image-mini-button" @click="reuseTurn(turn)">
                          <Icon name="copy" size="xs" />
                          {{ t('images.reuseConfig') }}
                        </button>
                        <button type="button" class="image-mini-icon-button" :title="t('images.deletePrompt')" @click="openDeletePromptConfirm(activeConversation.id, turn.id)">
                          <Icon name="trash" size="xs" />
                        </button>
                      </div>
                    </div>
                  </div>

                  <div v-if="!turn.resultsDeleted" class="flex justify-start">
                    <div class="w-full p-1">
                      <div v-if="turn.referenceImages.length > 0" class="mb-4 flex flex-col items-end">
                        <div class="mb-3 text-xs font-medium text-stone-500 dark:text-stone-400">{{ t('images.modeEdit') }}</div>
                        <div class="flex flex-wrap justify-end gap-3">
                          <div v-for="(image, index) in turn.referenceImages" :key="`${turn.id}-ref-${index}`" class="flex flex-col items-end gap-2">
                            <button
                              type="button"
                              class="group relative h-24 w-24 cursor-zoom-in overflow-hidden border border-stone-200/80 bg-stone-100/60 transition hover:border-stone-300 dark:border-white/10 dark:bg-stone-800"
                              @click="openReferenceImages(turn.referenceImages, `${turn.id}-ref`, index)"
                            >
                              <img :src="image.dataUrl" :alt="image.name" class="absolute inset-0 h-full w-full object-cover transition duration-200 group-hover:scale-[1.02]" />
                            </button>
                            <button type="button" class="image-mini-button" @click="continueEditWithReference(image)">
                              <Icon name="sparkles" size="xs" />
                              {{ t('images.continueEdit') }}
                            </button>
                          </div>
                        </div>
                      </div>

                      <div class="mb-3 flex flex-wrap items-center gap-2 text-xs text-stone-500 dark:text-stone-400 sm:mb-4">
                        <span class="image-meta-pill">{{ t('images.countLabel', { count: turn.count }) }}</span>
                        <span class="image-meta-pill">{{ turn.size }}</span>
                        <span class="image-meta-pill">{{ turn.quality }}</span>
                        <span
                          class="image-meta-pill"
                          :class="turn.status === 'queued' ? 'bg-amber-50 text-amber-700 dark:bg-amber-950/40 dark:text-amber-200' : turn.status === 'generating' ? 'bg-blue-50 text-blue-700 dark:bg-blue-950/40 dark:text-blue-200' : turn.status === 'success' ? 'bg-emerald-50 text-emerald-700 dark:bg-emerald-950/40 dark:text-emerald-200' : 'bg-red-50 text-red-700 dark:bg-red-950/40 dark:text-red-200'"
                        >
                          {{ turnStatusLabel(turn) }}
                        </span>
                      </div>

                      <div v-if="turn.status === 'generating'" class="space-y-4" role="status" aria-live="polite">
                        <div class="overflow-hidden border border-stone-200/80 bg-stone-50 dark:border-white/10 dark:bg-stone-900">
                          <div class="flex flex-col gap-3 p-4 sm:flex-row sm:items-center sm:justify-between">
                            <div class="flex min-w-0 items-start gap-3">
                              <div class="relative flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-full bg-white text-stone-700 shadow-sm dark:bg-stone-800 dark:text-stone-200">
                                <Icon name="sparkles" size="sm" />
                                <span class="absolute inset-0 rounded-full border border-stone-300 image-generating-pulse dark:border-stone-500"></span>
                              </div>
                              <div class="min-w-0">
                                <h3 class="text-sm font-semibold text-stone-950 dark:text-white">{{ t('images.generatingTitle') }}</h3>
                                <p class="mt-1 text-sm leading-6 text-stone-600 dark:text-stone-300">{{ generationStatusText(turn) }}</p>
                              </div>
                            </div>
                            <div class="inline-flex flex-shrink-0 items-center gap-2 rounded-full bg-white px-3 py-2 text-xs font-medium text-stone-600 shadow-sm dark:bg-stone-800 dark:text-stone-300">
                              <Icon name="clock" size="xs" />
                              {{ t('images.generatingElapsed', { time: generationElapsedLabel(turn) }) }}
                            </div>
                          </div>
                          <div class="h-1 overflow-hidden bg-stone-200 dark:bg-stone-800">
                            <div class="h-full w-1/3 rounded-full bg-stone-900 image-generating-progress dark:bg-white"></div>
                          </div>
                        </div>
                      </div>

                      <div class="grid grid-cols-3 gap-2 sm:block sm:columns-2 sm:gap-4 sm:space-y-4 xl:columns-3">
                        <div v-for="(image, index) in turn.images" :key="image.id" class="break-inside-avoid">
                          <LazyImage
                            v-if="image.status === 'success' && imageSrc(image)"
                            :src="imageSrc(image)"
                            :alt="image.revised_prompt || turn.prompt"
                            button-class="group block aspect-square w-full cursor-zoom-in overflow-hidden rounded-xl bg-stone-100 dark:bg-stone-900 sm:aspect-auto"
                            @open="openGeneratedImages(turn, index)"
                            @load="onImageLoad(image.id, $event)"
                          />
                          <div
                            v-else-if="image.status === 'error'"
                            class="overflow-hidden rounded-xl border border-rose-200 bg-rose-50 dark:border-rose-900/50 dark:bg-rose-950/30"
                            :class="placeholderAspectClass(turn)"
                          >
                            <div class="flex h-full min-h-16 flex-col items-center justify-center gap-1.5 px-2 py-2 text-center text-[11px] leading-4 text-rose-600 dark:text-rose-200 sm:gap-3 sm:px-6 sm:py-8 sm:text-sm sm:leading-6">
                              <p class="font-medium">{{ t('images.imageNofM', { index: index + 1, total: turn.images.length }) }}</p>
                              <span class="line-clamp-2 sm:line-clamp-none">{{ image.error || turn.error || t('images.generationFailed') }}</span>
                              <button type="button" class="rounded-full bg-white px-2 py-1 text-[10px] font-medium text-rose-600 shadow-sm transition hover:bg-rose-100 dark:bg-rose-950 dark:text-rose-200 dark:hover:bg-rose-900 sm:px-3 sm:text-xs" @click="retryImage(turn, image.id)">
                                {{ t('images.retryThisImage') }}
                              </button>
                            </div>
                          </div>
                          <div
                            v-else
                            class="relative overflow-hidden rounded-xl border border-stone-200/80 bg-stone-100/80 dark:border-white/10 dark:bg-stone-900"
                            :class="placeholderAspectClass(turn)"
                          >
                            <div class="absolute inset-0 image-generating-shimmer"></div>
                            <div class="relative z-10 flex h-full flex-col items-center justify-center gap-1.5 px-2 py-3 text-center text-stone-500 dark:text-stone-400 sm:gap-2 sm:px-6 sm:py-8">
                              <div class="rounded-full bg-white p-2 shadow-sm dark:bg-stone-800 sm:p-3">
                                <Icon v-if="!image.startedAt" name="clock" size="sm" />
                                <div v-else class="h-4 w-4 animate-spin rounded-full border-2 border-stone-200 border-b-stone-700 dark:border-stone-700 dark:border-b-stone-200 sm:h-5 sm:w-5"></div>
                              </div>
                              <p class="text-[11px] font-medium leading-4 sm:text-sm">{{ t('images.imageNofM', { index: index + 1, total: turn.images.length }) }}</p>
                              <p class="text-[10px] leading-4 text-stone-400 sm:text-xs">{{ image.startedAt ? t('images.generatingPlaceholder') : t('images.queued') }}</p>
                              <p v-if="imageElapsedLabel(image)" class="text-[10px] leading-4 text-stone-400 sm:text-xs">{{ imageElapsedLabel(image) }}</p>
                            </div>
                          </div>
                          <div class="flex flex-col gap-1 px-0.5 py-1 text-[10px] sm:flex-row sm:items-center sm:justify-between sm:gap-2 sm:px-3 sm:py-3 sm:text-xs">
                            <div class="min-w-0 text-stone-500 dark:text-stone-400">
                              <span>{{ t('images.resultNumber', { count: index + 1 }) }}</span>
                              <span v-if="image.durationMs != null" class="text-stone-400 sm:ml-2">{{ formatDuration(image.durationMs) }}</span>
                              <span v-if="imageMetaLabel(image)" class="block text-stone-400">{{ imageMetaLabel(image) }}</span>
                            </div>
                            <div class="flex items-center gap-1.5">
                              <button
                                v-if="image.status === 'success' && imageSrc(image)"
                                type="button"
                                class="image-result-action"
                                :title="t('images.continueEdit')"
                                @click="continueEditWithGenerated(image, index)"
                              >
                                <Icon name="sparkles" size="xs" />
                                <span class="hidden sm:inline">{{ t('images.continueEdit') }}</span>
                              </button>
                              <a
                                v-if="image.status === 'success' && imageSrc(image)"
                                :href="imageSrc(image)"
                                :download="`sub2api-image-${index + 1}.png`"
                                class="image-result-action"
                                :title="t('images.download')"
                                @click.stop
                              >
                                <Icon name="download" size="xs" />
                                <span class="hidden sm:inline">{{ t('images.download') }}</span>
                              </a>
                            </div>
                          </div>
                        </div>
                      </div>

                      <div v-if="turn.status === 'error' && turn.error" class="mt-4 flex items-center justify-between border-l-2 border-amber-300 bg-amber-50/70 px-4 py-3 text-sm leading-6 text-amber-700 dark:border-amber-500/50 dark:bg-amber-950/30 dark:text-amber-200">
                        <span>{{ turn.error }}</span>
                        <button type="button" class="ml-3 inline-flex shrink-0 items-center gap-1 rounded-full bg-amber-100 px-2.5 py-1 text-[11px] font-medium text-amber-700 transition hover:bg-amber-200 hover:text-amber-900 dark:bg-amber-900/40 dark:text-amber-200" @click="dismissTurnErrors(turn)">
                          <Icon name="eyeOff" size="xs" />
                          {{ t('images.dismissErrors') }}
                        </button>
                      </div>

                      <div class="mt-3 flex items-center gap-1.5 text-[11px] sm:mt-4">
                        <button type="button" class="image-mini-button" @click="regenerateTurn(turn)">
                          <Icon name="refresh" size="xs" />
                          {{ t('images.regenerateAll') }}
                        </button>
                        <button type="button" class="image-mini-icon-button" :title="t('images.deleteResults')" @click="openDeleteResultsConfirm(activeConversation.id, turn.id)">
                          <Icon name="trash" size="xs" />
                        </button>
                      </div>
                    </div>
                  </div>
                </article>
              </div>
            </div>
          </div>

          <div class="image-composer-wrap">
            <div v-if="referenceImages.length > 0" class="mb-2 flex gap-2 overflow-x-auto px-1 pb-1 sm:mb-3 sm:flex-wrap sm:overflow-visible sm:pb-0">
              <div v-for="(image, index) in referenceImages" :key="`${image.name}-${index}`" class="relative h-14 w-14 flex-shrink-0 sm:h-16 sm:w-16">
                <button
                  type="button"
                  class="group h-14 w-14 overflow-hidden rounded-2xl border border-stone-200 bg-stone-50 transition hover:border-stone-300 dark:border-white/10 dark:bg-stone-800 sm:h-16 sm:w-16"
                  @click="openReferenceImages(referenceImages, 'draft-ref', index)"
                >
                  <img :src="image.dataUrl" :alt="image.name" class="h-full w-full object-cover" />
                </button>
                <button
                  type="button"
                  class="absolute -right-1 -top-1 inline-flex h-5 w-5 items-center justify-center rounded-full border border-stone-200 bg-white text-stone-500 transition hover:border-stone-300 hover:text-stone-800 dark:border-white/10 dark:bg-stone-900 dark:text-stone-300"
                  :title="t('images.removeReference')"
                  @click="removeReference(index)"
                >
                  <Icon name="x" size="xs" />
                </button>
              </div>
            </div>

            <div
              class="image-composer-shell"
              :class="dragging ? 'image-composer-dragging' : ''"
              @dragenter.prevent="onDragEnter"
              @dragover.prevent="onDragOver"
              @dragleave="onDragLeave"
              @drop.prevent="onDrop"
              @click="focusPrompt"
            >
              <input ref="fileInputRef" type="file" accept="image/*" multiple class="hidden" @change="onFileInput" />
              <textarea
                ref="textareaRef"
                v-model="prompt"
                class="image-prompt-input"
                :placeholder="referenceImages.length > 0 ? t('images.editPlaceholder') : t('images.promptPlaceholder')"
                @paste="onPaste"
                @keydown.enter.exact.prevent="submitPrompt"
              ></textarea>

              <div v-if="dragging" class="image-drop-overlay">
                <div class="inline-flex items-center gap-2 rounded-full bg-stone-950 px-4 py-2 text-white shadow-lg dark:bg-white dark:text-stone-950">
                  <Icon name="upload" size="sm" />
                  {{ t('images.uploadReference') }}
                </div>
              </div>

              <div class="image-composer-controls" @click.stop>
                <div class="image-control-row">
                  <button
                    type="button"
                    class="image-pill-button"
                    :title="referenceImages.length > 0 ? t('images.addReference') : t('images.uploadReference')"
                    @click="fileInputRef?.click()"
                  >
                    <Icon name="upload" size="sm" />
                    <span class="hidden sm:inline">{{ referenceImages.length > 0 ? t('images.addReference') : t('images.uploadReference') }}</span>
                  </button>

                  <div class="image-key-chip" :title="selectedKeyLabel">
                    <Icon name="key" size="xs" />
                    <span class="truncate">{{ selectedKeyLabel }}</span>
                  </div>

                  <div class="image-settings-anchor" @click.stop>
                    <button
                      ref="settingsButtonRef"
                      type="button"
                      class="image-settings-button"
                      :title="composerSettingsLabel"
                      :aria-expanded="settingsOpen"
                      @click="toggleSettingsPanel"
                    >
                      <span class="truncate">{{ composerSettingsLabel }}</span>
                      <Icon name="chevronDown" size="xs" class="flex-shrink-0 transition-transform" :class="settingsOpen ? 'rotate-180' : ''" />
                    </button>

                    <div
                      v-if="settingsOpen"
                      ref="settingsPanelRef"
                      class="image-settings-panel"
                      :style="settingsPanelStyle"
                      @click.stop
                    >
                      <h3 class="mb-3 text-base font-semibold text-stone-950 dark:text-white">{{ t('images.settingsTitle') }}</h3>

                      <div class="space-y-4">
                        <div>
                          <div class="mb-2 text-sm font-medium text-stone-900 dark:text-stone-100">{{ t('images.apiKey') }}</div>
                          <Select
                            :model-value="selectedKeyId"
                            :options="keyOptions"
                            :placeholder="t('images.selectKey')"
                            :searchable="true"
                            @update:model-value="handleKeyChange"
                          >
                            <template #selected="{ option }">
                              <span class="flex min-w-0 items-center gap-2">
                                <Icon name="key" size="xs" class="flex-shrink-0 text-stone-500" />
                                <span class="truncate">{{ option?.label || t('images.selectKey') }}</span>
                              </span>
                            </template>
                          </Select>
                        </div>

                        <div>
                          <div class="mb-2 text-sm font-medium text-stone-900 dark:text-stone-100">{{ t('images.model') }}</div>
                          <Select
                            :model-value="selectedModel"
                            :options="modelOptions"
                            :placeholder="modelsLoading ? t('images.loadingModels') : t('images.selectModel')"
                            :disabled="modelsLoading || modelOptions.length === 0"
                            :searchable="true"
                            @update:model-value="handleModelChange"
                          >
                            <template #selected="{ option }">
                              <span class="flex min-w-0 items-center gap-2">
                                <Icon name="sparkles" size="xs" class="flex-shrink-0 text-stone-500" />
                                <span class="truncate">{{ option?.label || selectedModel }}</span>
                              </span>
                            </template>
                          </Select>
                        </div>

                        <div>
                          <div class="mb-2 text-sm font-medium text-stone-900 dark:text-stone-100">{{ t('images.qualityLabel') }}</div>
                          <div class="grid grid-cols-4 gap-2">
                            <button
                              v-for="option in qualityOptions"
                              :key="String(option.value)"
                              type="button"
                              class="image-choice-button"
                              :class="quality === option.value ? 'image-choice-button-active' : ''"
                              @click="quality = String(option.value)"
                            >
                              {{ option.label }}
                            </button>
                          </div>
                        </div>

                        <div>
                          <div class="mb-2 text-sm font-medium text-stone-900 dark:text-stone-100">{{ t('images.sizeLabel') }}</div>
                          <div class="grid grid-cols-[1fr_auto_1fr] items-center gap-2">
                            <label class="image-dimension-field">
                              <span>W</span>
                              <input :value="imageWidth" type="number" min="1" inputmode="numeric" @input="updateImageWidth(($event.target as HTMLInputElement).value)" />
                            </label>
                            <span class="text-stone-400">x</span>
                            <label class="image-dimension-field">
                              <span>H</span>
                              <input :value="imageHeight" type="number" min="1" inputmode="numeric" @input="updateImageHeight(($event.target as HTMLInputElement).value)" />
                            </label>
                          </div>
                        </div>

                        <div>
                          <div class="mb-2 text-sm font-medium text-stone-900 dark:text-stone-100">{{ t('images.aspectRatio') }}</div>
                          <div class="grid grid-cols-4 gap-2 sm:grid-cols-5">
                            <button
                              v-for="option in aspectOptions"
                              :key="`${option.ratio}-${option.tier}-${option.width}-${option.height}`"
                              type="button"
                              class="image-aspect-button"
                              :class="[
                                option.ratio === imageRatio && option.tier === imageTier && option.width === imageWidth && option.height === imageHeight ? 'image-choice-button-active' : '',
                                option.disabled ? 'image-aspect-button-disabled' : '',
                              ]"
                              :disabled="option.disabled"
                              @click="selectAspect(option)"
                            >
                              <Icon v-if="option.ratio === '1:1'" name="grid" size="xs" />
                              <Icon v-else-if="option.ratio.includes('16') || option.ratio.includes('3:2') || option.ratio.includes('4:3')" name="arrowsUpDown" size="xs" class="rotate-90" />
                              <Icon v-else-if="option.ratio !== 'auto'" name="arrowsUpDown" size="xs" />
                              <span>{{ option.label }}</span>
                            </button>
                          </div>
                        </div>

                        <div class="border-t border-stone-100 pt-3 dark:border-white/10">
                          <div class="mb-2 text-sm font-medium text-stone-900 dark:text-stone-100">{{ t('images.countSetting') }}</div>
                          <div class="grid grid-cols-4 gap-2 sm:grid-cols-5">
                            <button
                              v-for="option in countOptions"
                              :key="option"
                              type="button"
                              class="image-choice-button"
                              :class="count === option ? 'image-choice-button-active' : ''"
                              @click="setCount(option)"
                            >
                              {{ t('images.countLabel', { count: option }) }}
                            </button>
                            <input
                              :value="count"
                              type="number"
                              min="1"
                              max="100"
                              step="1"
                              inputmode="numeric"
                              class="image-count-input"
                              @input="setCount(($event.target as HTMLInputElement).value)"
                            />
                          </div>
                        </div>
                      </div>
                    </div>
                  </div>

                  <div v-if="activeTaskCount > 0" class="image-task-pill">
                    <div class="h-3 w-3 animate-spin rounded-full border-2 border-amber-200 border-b-amber-700"></div>
                    {{ activeTaskCount }}
                  </div>
                </div>

                <button
                  type="button"
                  class="image-send-button"
                  :disabled="!canSubmit"
                  :title="generating ? t('images.generating') : referenceImages.length > 0 ? t('images.editImage') : t('images.generate')"
                  @click="submitPrompt"
                >
                  <Icon name="arrowUp" size="sm" />
                </button>
              </div>
            </div>
          </div>
        </section>
      </div>
    </div>

    <div v-if="mobileHistoryOpen" class="fixed inset-0 z-[100000010] flex items-center justify-center bg-black/45 p-4 lg:hidden" @click.self="mobileHistoryOpen = false">
      <div class="flex h-[min(82dvh,760px)] w-[92vw] max-w-[460px] flex-col overflow-hidden rounded-[32px] border border-white/80 bg-white shadow-[0_32px_110px_-38px_rgba(15,23,42,0.45)] dark:border-white/10 dark:bg-stone-950">
        <div class="flex items-center justify-between px-6 pb-4 pt-7">
          <div class="flex items-center gap-2 text-xl font-bold text-stone-950 dark:text-white">
            <Icon name="clock" size="md" />
            {{ t('images.history') }}
          </div>
          <button type="button" class="image-icon-button h-9 w-9 rounded-xl" @click="mobileHistoryOpen = false">
            <Icon name="x" size="sm" />
          </button>
        </div>
        <div class="flex items-center gap-2 px-6 pb-3">
          <button type="button" class="image-primary-button h-10 flex-1 rounded-xl" @click="startNewConversation(); mobileHistoryOpen = false">
            <Icon name="plus" size="sm" />
            {{ t('images.newChat') }}
          </button>
          <button type="button" class="image-icon-button h-10 w-10 rounded-xl" :disabled="conversations.length === 0" @click="openClearHistoryConfirm">
            <Icon name="trash" size="sm" />
          </button>
        </div>
        <div class="min-h-0 flex-1 overflow-y-auto px-5 pb-8">
          <p v-if="conversations.length === 0" class="px-2 py-3 text-sm leading-6 text-stone-500 dark:text-stone-400">
            {{ t('images.emptyHistory') }}
          </p>
          <div
            v-for="conversation in conversations"
            v-else
            :key="conversation.id"
            class="group relative border-l-2 px-4 py-3.5 transition-colors"
            :class="conversation.id === activeConversationId ? 'border-stone-950 bg-black/[0.035] text-stone-950 dark:border-white dark:bg-white/10 dark:text-white' : 'border-transparent text-stone-700 hover:border-stone-300 hover:bg-stone-50 dark:text-stone-300 dark:hover:border-stone-600 dark:hover:bg-white/5'"
          >
            <button type="button" class="block w-full pr-16 text-left" @click="activeConversationId = conversation.id; mobileHistoryOpen = false">
              <input
                v-if="editingConversationId === conversation.id"
                v-model="editingConversationTitle"
                class="w-full truncate rounded-md border border-stone-300 bg-white px-1.5 py-0.5 text-base font-semibold text-stone-900 outline-none focus:border-stone-500 dark:border-white/10 dark:bg-stone-900 dark:text-white"
                @click.stop
                @blur="commitRenameConversation"
                @keydown.enter.prevent="commitRenameConversation"
                @keydown.esc.prevent="cancelRenameConversation"
              />
              <span v-else class="block truncate text-base font-semibold">{{ conversation.title }}</span>
              <span class="mt-1 block text-xs text-stone-400">{{ conversation.turns.length }} · {{ formatTime(conversation.updatedAt) }}</span>
              <span v-if="conversationStats(conversation).running > 0 || conversationStats(conversation).queued > 0" class="mt-2 flex flex-wrap gap-1.5 text-[11px]">
                <span v-if="conversationStats(conversation).running > 0" class="rounded-full bg-blue-50 px-2 py-0.5 text-blue-600 dark:bg-blue-950/40 dark:text-blue-200">{{ t('images.processingCount', { count: conversationStats(conversation).running }) }}</span>
                <span v-if="conversationStats(conversation).queued > 0" class="rounded-full bg-amber-50 px-2 py-0.5 text-amber-700 dark:bg-amber-950/40 dark:text-amber-200">{{ t('images.queuedCount', { count: conversationStats(conversation).queued }) }}</span>
              </span>
            </button>
            <div class="absolute right-1.5 top-3 flex items-center gap-0.5">
              <button
                type="button"
                class="inline-flex h-7 w-7 items-center justify-center rounded-lg text-stone-400 hover:bg-stone-100 hover:text-stone-700 dark:hover:bg-white/10 dark:hover:text-white"
                :title="t('images.renameConversation')"
                @click="startRenameConversation(conversation, $event)"
              >
                <Icon name="edit" size="xs" />
              </button>
              <button
                type="button"
                class="inline-flex h-7 w-7 items-center justify-center rounded-lg text-stone-400 hover:bg-stone-100 hover:text-red-500 dark:hover:bg-white/10"
                :title="t('common.delete')"
                @click="openDeleteConversationConfirm(conversation.id)"
              >
                <Icon name="trash" size="xs" />
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>

    <div v-if="deleteConfirm" class="fixed inset-0 z-[100000025] flex items-center justify-center bg-black/45 p-4" @click.self="deleteConfirm = null">
      <div class="w-full max-w-md rounded-2xl border border-stone-200 bg-white p-6 shadow-2xl dark:border-white/10 dark:bg-stone-950">
        <h3 class="text-base font-semibold text-stone-950 dark:text-white">{{ deleteConfirmTitle }}</h3>
        <p class="mt-2 text-sm leading-6 text-stone-500 dark:text-stone-400">{{ deleteConfirmDescription }}</p>
        <div class="mt-6 flex justify-end gap-2">
          <button type="button" class="image-soft-button" @click="deleteConfirm = null">{{ t('common.cancel') }}</button>
          <button type="button" class="image-danger-button" @click="confirmDelete">{{ t('common.confirm') }}</button>
        </div>
      </div>
    </div>

    <ImageLightbox v-model:open="lightbox.open" v-model:index="lightbox.index" :images="lightbox.items" />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import Select from '@/components/common/Select.vue'
import LazyImage from '@/components/common/LazyImage.vue'
import ImageLightbox, { type LightboxItem } from '@/components/common/ImageLightbox.vue'
import { useAppStore } from '@/stores'
import keysAPI from '@/api/keys'
import { createImageEdit, createImageGeneration, listImageModels, type ImageResponseItem } from '@/api/images'
import type { ApiKey } from '@/types'
import { listImageCapableAPIKeys, pickDefaultImageAPIKey } from '@/utils/imageKeys'
import {
  buildImageConversationTitle,
  clearImageConversations,
  createImageConversationId,
  deleteImageConversation,
  listImageConversations,
  renameImageConversation,
  saveImageConversations,
  upsertImageConversation,
  type ImageConversation,
  type ImageTurn,
  type ImageTurnStatus,
  type StoredGeneratedImage,
  type StoredReferenceImage,
} from '@/stores/imageConversations'

const { t } = useI18n()
const appStore = useAppStore()

const loadingKeys = ref(true)
const modelsLoading = ref(false)
const modelLoadError = ref('')
const apiKeys = ref<ApiKey[]>([])
const selectedKeyId = ref<number | null>(null)
const selectedModel = ref('gpt-image-2')
const imageModels = ref<string[]>(['gpt-image-2'])
const prompt = ref('')
const size = ref('1024x1024')
const imageRatio = ref('1:1')
const imageTier = ref('1k')
const imageWidth = ref('1024')
const imageHeight = ref('1024')
const quality = ref('auto')
const count = ref(1)
const referenceImages = ref<StoredReferenceImage[]>([])
const referenceFiles = ref<File[]>([])
const conversations = ref<ImageConversation[]>([])
const activeConversationId = ref<string | null>(null)
const mobileHistoryOpen = ref(false)
const dragging = ref(false)
const settingsOpen = ref(false)
const editingConversationId = ref<string | null>(null)
const editingConversationTitle = ref('')
const deleteConfirm = ref<
  | { type: 'one'; conversationId: string }
  | { type: 'prompt'; conversationId: string; turnId: string }
  | { type: 'results'; conversationId: string; turnId: string }
  | { type: 'all' }
  | null
>(null)
const fileInputRef = ref<HTMLInputElement | null>(null)
const textareaRef = ref<HTMLTextAreaElement | null>(null)
const resultPanelRef = ref<HTMLElement | null>(null)
const settingsButtonRef = ref<HTMLButtonElement | null>(null)
const settingsPanelRef = ref<HTMLElement | null>(null)
const nowMs = ref(Date.now())
let modelsAbortController: AbortController | null = null
let generationTicker: number | null = null

// Client-side generation queue: each image is an independent n=1 job, run with a
// small concurrency cap so multiple turns/images stay non-blocking and partial
// failures can be retried individually (the gateway is synchronous-only).
const MAX_CONCURRENT_JOBS = 2
interface ImageJob {
  conversationId: string
  turnId: string
  imageId: string
  apiKey: string
  prompt: string
  model: string
  mode: 'generate' | 'edit'
  size: string
  quality: string
  files: File[]
}
const jobQueue: ImageJob[] = []
let activeJobs = 0

const lightbox = reactive<{
  open: boolean
  index: number
  items: LightboxItem[]
}>({
  open: false,
  index: 0,
  items: [],
})

// Natural image dimensions captured on <img> load (session-only, not persisted).
const imageDimensions = reactive<Record<string, string>>({})

const imageKeys = computed(() => listImageCapableAPIKeys(apiKeys.value))
const selectedKey = computed(() => imageKeys.value.find((key) => key.id === selectedKeyId.value) || null)
const activeConversation = computed(() => conversations.value.find((item) => item.id === activeConversationId.value) || null)
const activeTaskCount = computed(() =>
  conversations.value.reduce(
    (sum, conversation) => sum + conversation.turns.filter((turn) => turn.status === 'queued' || turn.status === 'generating').length,
    0
  )
)
const generating = computed(() => activeTaskCount.value > 0)

const keyOptions = computed(() =>
  imageKeys.value.map((key) => ({
    value: key.id,
    label: key.name,
  }))
)

const modelOptions = computed(() => imageModels.value.map((model) => ({ value: model, label: model })))

const isCodexImageModel = computed(() => selectedModel.value.toLowerCase().includes('codex'))

const aspectOptions = computed(() =>
  [
    { ratio: '1:1', tier: '1k', width: '1024', height: '1024', label: '1:1' },
    { ratio: '2:3', tier: '1k', width: '1024', height: '1536', label: '2:3' },
    { ratio: '3:2', tier: '1k', width: '1536', height: '1024', label: '3:2' },
    { ratio: '3:4', tier: '1k', width: '1024', height: '1365', label: '3:4' },
    { ratio: '4:3', tier: '1k', width: '1365', height: '1024', label: '4:3' },
    { ratio: '9:16', tier: '1k', width: '1088', height: '1920', label: '9:16' },
    { ratio: '16:9', tier: '1k', width: '1920', height: '1088', label: '16:9' },
    { ratio: '1:1', tier: '2k', width: '2048', height: '2048', label: '1:1(2k)' },
    { ratio: '16:9', tier: '2k', width: '2560', height: '1440', label: '16:9(2k)' },
    { ratio: '9:16', tier: '2k', width: '1440', height: '2560', label: '9:16(2k)' },
    { ratio: '16:9', tier: '4k', width: '3840', height: '2160', label: '16:9(4k)' },
    { ratio: '9:16', tier: '4k', width: '2160', height: '3840', label: '9:16(4k)' },
    { ratio: 'auto', tier: 'auto', width: '1024', height: '1024', label: 'auto' },
  ].map((option) => ({
    ...option,
    disabled: !isCodexImageModel.value && (option.tier === '2k' || option.tier === '4k'),
  }))
)

const qualityOptions = computed(() => [
  { value: 'auto', label: t('images.quality.auto') },
  { value: 'low', label: t('images.quality.low') },
  { value: 'medium', label: t('images.quality.medium') },
  { value: 'high', label: t('images.quality.high') },
])

const countOptions = computed(() =>
  Array.from({ length: 10 }, (_, index) => index + 1)
)

const selectedSizeLabel = computed(() => {
  const option = aspectOptions.value.find((item) => item.width === imageWidth.value && item.height === imageHeight.value && item.ratio === imageRatio.value && item.tier === imageTier.value)
  return option?.label || `${imageWidth.value}x${imageHeight.value}`
})
const selectedQualityLabel = computed(() => qualityOptions.value.find((option) => option.value === quality.value)?.label || quality.value)
const selectedCountLabel = computed(() => t('images.countLabel', { count: count.value }))
const composerSettingsLabel = computed(() => `${selectedQualityLabel.value} · ${selectedSizeLabel.value} · ${selectedCountLabel.value}`)
const selectedKeyLabel = computed(() => selectedKey.value?.name || t('images.selectKey'))
const settingsPanelStyle = computed(() => {
  const panelWidth = 'min(460px, calc(100vw - 2rem))'
  const button = settingsButtonRef.value
  if (!button) return { width: panelWidth }
  const rect = button.getBoundingClientRect()
  const viewportWidth = window.innerWidth || 0
  const maxPanelWidth = Math.min(460, Math.max(0, viewportWidth - 32))
  const left = Math.max(16, Math.min(rect.left, viewportWidth - maxPanelWidth - 16))
  return {
    width: panelWidth,
    top: `${rect.top - 8}px`,
    left: `${left}px`,
  }
})

const deleteConfirmTitle = computed(() => {
  if (deleteConfirm.value?.type === 'all') return t('images.clearHistoryConfirmTitle')
  if (deleteConfirm.value?.type === 'prompt') return t('images.deletePromptConfirmTitle')
  if (deleteConfirm.value?.type === 'results') return t('images.deleteResultsConfirmTitle')
  if (deleteConfirm.value?.type === 'one') return t('images.deleteConversationConfirmTitle')
  return ''
})

const deleteConfirmDescription = computed(() => {
  if (deleteConfirm.value?.type === 'all') return t('images.clearHistoryConfirmDescription')
  if (deleteConfirm.value?.type === 'prompt') return t('images.deletePromptConfirmDescription')
  if (deleteConfirm.value?.type === 'results') return t('images.deleteResultsConfirmDescription')
  if (deleteConfirm.value?.type === 'one') return t('images.deleteConversationConfirmDescription')
  return ''
})

const canSubmit = computed(() => {
  return Boolean(prompt.value.trim() && selectedKey.value && selectedModel.value)
})

watch(selectedKey, (key) => {
  if (key) {
    void loadModelsForKey(key)
  }
})

onMounted(() => {
  conversations.value = recoverOrphanConversations(listImageConversations())
  activeConversationId.value = conversations.value[0]?.id ?? null
  generationTicker = window.setInterval(() => {
    if (conversations.value.some((conversation) => conversation.turns.some((turn) => turn.images.some((image) => image.status === 'loading')))) {
      nowMs.value = Date.now()
    }
  }, 500)
  window.addEventListener('mousedown', handleWindowPointerDown)
  window.addEventListener('resize', closeSettingsPanel)
  void loadKeys()
})

onUnmounted(() => {
  modelsAbortController?.abort()
  window.removeEventListener('mousedown', handleWindowPointerDown)
  window.removeEventListener('resize', closeSettingsPanel)
  if (generationTicker !== null) {
    window.clearInterval(generationTicker)
    generationTicker = null
  }
})

async function loadKeys() {
  loadingKeys.value = true
  try {
    const result = await keysAPI.list(1, 1000, { status: 'active' })
    apiKeys.value = result.items
    const nextSelected = pickDefaultImageAPIKey(result.items, selectedKeyId.value)
    selectedKeyId.value = nextSelected?.id ?? null
    if (!nextSelected) {
      imageModels.value = ['gpt-image-2']
      selectedModel.value = 'gpt-image-2'
    }
  } catch (err) {
    appStore.showError(t('images.loadKeysFailed'))
  } finally {
    loadingKeys.value = false
  }
}

async function loadModelsForKey(key: ApiKey) {
  modelsAbortController?.abort()
  const controller = new AbortController()
  modelsAbortController = controller
  modelsLoading.value = true
  modelLoadError.value = ''
  try {
    const models = await listImageModels({
      apiKey: key.key,
      signal: controller.signal,
    })
    if (controller.signal.aborted) return
    imageModels.value = models.length > 0 ? models : ['gpt-image-2']
    if (!imageModels.value.includes(selectedModel.value)) {
      selectedModel.value = imageModels.value[0]
    }
  } catch (err) {
    if ((err as { name?: string }).name === 'AbortError') return
    imageModels.value = ['gpt-image-2']
    selectedModel.value = 'gpt-image-2'
    modelLoadError.value = t('images.loadModelsFailed')
  } finally {
    if (modelsAbortController === controller) {
      modelsLoading.value = false
    }
  }
}

function handleKeyChange(value: string | number | boolean | null) {
  selectedKeyId.value = typeof value === 'number' ? value : Number(value || 0) || null
}

function handleModelChange(value: string | number | boolean | null) {
  selectedModel.value = String(value || 'gpt-image-2')
}

async function toggleSettingsPanel() {
  settingsOpen.value = !settingsOpen.value
  if (settingsOpen.value) {
    await nextTick()
  }
}

function closeSettingsPanel() {
  settingsOpen.value = false
}

function handleWindowPointerDown(event: MouseEvent) {
  if (!settingsOpen.value) return
  const target = event.target
  if (!(target instanceof Node)) return
  const panel = settingsPanelRef.value
  const button = settingsButtonRef.value
  if (panel?.contains(target) || button?.contains(target)) return
  const element = target instanceof Element ? target : null
  if (element?.closest('.select-dropdown-portal')) return
  closeSettingsPanel()
}

function submitPrompt() {
  if (!canSubmit.value || !selectedKey.value) return
  const key = selectedKey.value
  const promptText = prompt.value.trim()
  const mode: 'generate' | 'edit' = referenceFiles.value.length > 0 ? 'edit' : 'generate'
  const now = new Date().toISOString()
  const conversationID = activeConversationId.value || createImageConversationId()
  const existing = conversations.value.find((item) => item.id === conversationID)
  const turnID = createImageConversationId()
  const files = mode === 'edit' ? [...referenceFiles.value] : []
  const draftTurn: ImageTurn = {
    id: turnID,
    prompt: promptText,
    model: selectedModel.value,
    mode,
    referenceImages: mode === 'edit' ? referenceImages.value : [],
    count: count.value,
    size: size.value,
    ratio: imageRatio.value,
    tier: imageTier.value,
    quality: quality.value,
    images: createLoadingImages(turnID, count.value),
    status: 'queued',
    createdAt: now,
  }
  const draftConversation: ImageConversation = existing
    ? { ...existing, updatedAt: now, turns: [...existing.turns, draftTurn] }
    : {
        id: conversationID,
        title: buildImageConversationTitle(promptText),
        createdAt: now,
        updatedAt: now,
        turns: [draftTurn],
      }

  conversations.value = upsertImageConversation(draftConversation, conversations.value)
  activeConversationId.value = conversationID
  prompt.value = ''
  referenceImages.value = []
  referenceFiles.value = []
  void scrollToLatest()

  const storedTurn = conversations.value.find((item) => item.id === conversationID)?.turns.find((item) => item.id === turnID)
  if (storedTurn) {
    enqueueImageJobs(
      storedTurn.images
        .filter((image) => image.status === 'loading')
        .map((image) => buildJob(conversationID, storedTurn, image.id, key.key, files))
    )
  }
}

function buildJob(conversationID: string, turn: ImageTurn, imageID: string, apiKey: string, files: File[]): ImageJob {
  return {
    conversationId: conversationID,
    turnId: turn.id,
    imageId: imageID,
    apiKey,
    prompt: turn.prompt,
    model: turn.model,
    mode: turn.mode,
    size: turn.size,
    quality: turn.quality,
    files,
  }
}

function enqueueImageJobs(jobs: ImageJob[]) {
  for (const job of jobs) {
    jobQueue.push(job)
  }
  pumpJobs()
}

function pumpJobs() {
  while (activeJobs < MAX_CONCURRENT_JOBS && jobQueue.length > 0) {
    const job = jobQueue.shift()
    if (job) void runJob(job)
  }
}

async function runJob(job: ImageJob) {
  activeJobs += 1
  const startedAt = Date.now()
  updateImage(job.conversationId, job.turnId, job.imageId, { status: 'loading', startedAt, error: undefined })
  recomputeTurnStatus(job.conversationId, job.turnId)
  try {
    const result = await requestImages({
      apiKey: job.apiKey,
      mode: job.mode,
      files: job.files,
      prompt: job.prompt,
      model: job.model,
      size: job.size,
      quality: job.quality,
    })
    const [item] = normalizeResponseImages(result.data)
    if (!item) throw new Error(t('images.noImageData'))
    updateImage(job.conversationId, job.turnId, job.imageId, {
      status: 'success',
      b64_json: item.b64_json,
      url: item.url,
      revised_prompt: item.revised_prompt,
      error: undefined,
      durationMs: Date.now() - startedAt,
    })
  } catch (err) {
    const message = extractErrorMessage(err, t('images.generationFailed'))
    updateImage(job.conversationId, job.turnId, job.imageId, {
      status: 'error',
      error: message,
      durationMs: Date.now() - startedAt,
    })
  } finally {
    activeJobs -= 1
    recomputeTurnStatus(job.conversationId, job.turnId)
    pumpJobs()
  }
}

async function requestImages(options: {
  apiKey: string
  mode: 'generate' | 'edit'
  files: File[]
  prompt: string
  model: string
  size: string
  quality: string
}) {
  return options.mode === 'edit'
    ? createImageEdit({
        apiKey: options.apiKey,
        files: options.files,
        prompt: options.prompt,
        model: options.model,
        size: options.size,
        quality: options.quality,
        n: 1,
      })
    : createImageGeneration({
        apiKey: options.apiKey,
        prompt: options.prompt,
        model: options.model,
        size: options.size,
        quality: options.quality,
        n: 1,
      })
}

function updateImage(conversationID: string, turnID: string, imageID: string, patch: Partial<StoredGeneratedImage>) {
  const conversation = conversations.value.find((item) => item.id === conversationID)
  const turn = conversation?.turns.find((item) => item.id === turnID)
  if (!turn) return
  const images = turn.images.map((image) => (image.id === imageID ? { ...image, ...patch } : image))
  updateTurn(conversationID, turnID, { images })
}

function recomputeTurnStatus(conversationID: string, turnID: string) {
  const conversation = conversations.value.find((item) => item.id === conversationID)
  const turn = conversation?.turns.find((item) => item.id === turnID)
  if (!turn) return
  const loadingImages = turn.images.filter((image) => image.status === 'loading')
  let status: ImageTurnStatus
  if (loadingImages.length > 0) {
    status = loadingImages.some((image) => image.startedAt) ? 'generating' : 'queued'
  } else if (turn.images.some((image) => image.status === 'error')) {
    status = 'error'
  } else {
    status = 'success'
  }
  const error =
    status === 'error'
      ? turn.error || turn.images.find((image) => image.status === 'error')?.error || t('images.generationFailed')
      : undefined
  if (status !== turn.status || error !== turn.error) {
    updateTurn(conversationID, turnID, { status, error })
  }
}

// On reload, any image left "loading" lost its in-flight request (sync backend has
// no resumable task), so surface it as a retryable error instead of spinning forever.
function recoverOrphanConversations(items: ImageConversation[]): ImageConversation[] {
  let mutated = false
  const recovered = items.map((conversation) => {
    let changed = false
    const turns = conversation.turns.map((turn) => {
      if (!turn.images.some((image) => image.status === 'loading')) return turn
      changed = true
      const images = turn.images.map((image) =>
        image.status === 'loading'
          ? { ...image, status: 'error' as const, error: t('images.interrupted'), startedAt: undefined }
          : image
      )
      return {
        ...turn,
        images,
        status: 'error' as const,
        error: turn.error || t('images.interrupted'),
      }
    })
    if (changed) mutated = true
    return changed ? { ...conversation, turns } : conversation
  })
  if (mutated) {
    saveImageConversations(recovered)
  }
  return recovered
}

function updateTurn(conversationID: string, turnID: string, patch: Partial<ImageTurn>) {
  const current = conversations.value.find((item) => item.id === conversationID)
  if (!current) return
  const updated: ImageConversation = {
    ...current,
    updatedAt: new Date().toISOString(),
    turns: current.turns.map((turn) => (turn.id === turnID ? { ...turn, ...patch } : turn)),
  }
  conversations.value = upsertImageConversation(updated, conversations.value)
}

function normalizeResponseImages(items: ImageResponseItem[] = []): StoredGeneratedImage[] {
  return items.map((item): StoredGeneratedImage => ({
    id: createImageConversationId(),
    status: 'success' as const,
    b64_json: item.url ? undefined : item.b64_json,
    url: item.url,
    revised_prompt: item.revised_prompt,
    error: undefined,
  })).filter((item) => item.b64_json || item.url)
}

function createLoadingImages(turnID: string, imageCount: number): StoredGeneratedImage[] {
  return Array.from({ length: clampImageCount(imageCount) }, (_, index) => ({
    id: `${turnID}-${index + 1}`,
    status: 'loading' as const,
  }))
}

function clampImageCount(value: number): number {
  return Math.min(100, Math.max(1, Math.floor(Number(value) || 1)))
}

function generationElapsedSeconds(turn: ImageTurn): number {
  const startedAt = new Date(turn.createdAt).getTime()
  if (!Number.isFinite(startedAt)) return 0
  return Math.max(0, Math.floor((nowMs.value - startedAt) / 1000))
}

function generationElapsedLabel(turn: ImageTurn): string {
  const elapsed = generationElapsedSeconds(turn)
  if (elapsed < 60) return `${elapsed}s`
  const minutes = Math.floor(elapsed / 60)
  const seconds = elapsed % 60
  return `${minutes}m ${seconds}s`
}

function generationStatusText(turn: ImageTurn): string {
  const elapsed = generationElapsedSeconds(turn)
  if (elapsed < 8) return t('images.generatingQueued')
  if (elapsed < 45) return t('images.generatingRendering')
  return t('images.generatingFinalizing')
}

function turnStatusLabel(turn: ImageTurn): string {
  if (turn.status === 'queued') return t('images.queued')
  if (turn.status === 'generating') return t('images.generating')
  if (turn.status === 'success') return t('images.generationComplete')
  return t('images.generationFailed')
}

function turnModeLabel(turn: ImageTurn): string {
  return turn.mode === 'edit' ? t('images.modeEdit') : t('images.modeGenerate')
}

function imageSrc(image: ImageResponseItem): string {
  if (image.b64_json) return `data:image/png;base64,${image.b64_json}`
  return image.url || ''
}

function onImageLoad(imageID: string, payload: { width: number; height: number }) {
  imageDimensions[imageID] = `${payload.width} x ${payload.height}`
}

function formatDuration(ms: number): string {
  return `${(ms / 1000).toFixed(1)}s`
}

function formatBase64ImageSize(base64: string): string {
  const normalized = base64.replace(/\s/g, '')
  const padding = normalized.endsWith('==') ? 2 : normalized.endsWith('=') ? 1 : 0
  const bytes = Math.max(0, Math.floor((normalized.length * 3) / 4) - padding)
  if (bytes >= 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(2)} MB`
  if (bytes >= 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${bytes} B`
}

function imageMetaLabel(image: StoredGeneratedImage): string {
  const sizeLabel = image.b64_json ? formatBase64ImageSize(image.b64_json) : ''
  return [sizeLabel, imageDimensions[image.id]].filter(Boolean).join(' · ')
}

function imageElapsedLabel(image: StoredGeneratedImage): string {
  if (!image.startedAt) return ''
  const elapsed = Math.max(0, (nowMs.value - image.startedAt) / 1000)
  return `${elapsed.toFixed(1)}s`
}

function placeholderAspectClass(turn: ImageTurn): string {
  if (turn.ratio === '16:9') return 'aspect-square sm:aspect-video'
  if (turn.ratio === '9:16') return 'aspect-square sm:aspect-[9/16]'
  if (turn.ratio === '4:3') return 'aspect-square sm:aspect-[4/3]'
  if (turn.ratio === '3:4') return 'aspect-square sm:aspect-[3/4]'
  return 'aspect-square sm:aspect-square'
}

async function continueEditWithGenerated(image: ImageResponseItem, index: number) {
  const src = imageSrc(image)
  if (!src) return
  try {
    const file = await imageResponseItemToFile(image, `generated-${index + 1}.png`)
    await addReferenceFiles([file])
    appStore.showSuccess(t('images.addedToEdit'))
    await scrollToComposer()
  } catch {
    appStore.showError(t('images.readReferenceFailed'))
  }
}

async function continueEditWithReference(image: StoredReferenceImage) {
  try {
    await addReferenceFiles([dataUrlToFile(image.dataUrl, image.name || 'reference.png', image.type)])
    appStore.showSuccess(t('images.addedToEdit'))
    await scrollToComposer()
  } catch {
    appStore.showError(t('images.readReferenceFailed'))
  }
}

function reuseTurn(turn: ImageTurn) {
  prompt.value = turn.prompt
  selectedModel.value = turn.model
  setImageSizeFromTurn(turn)
  quality.value = turn.quality
  count.value = turn.count
  referenceImages.value = [...turn.referenceImages]
  referenceFiles.value = turn.referenceImages.map((image, index) => dataUrlToFile(image.dataUrl, image.name || `reference-${index + 1}.png`, image.type))
  void scrollToComposer()
}

function regenerateTurn(turn: ImageTurn) {
  reuseTurn(turn)
  void submitPrompt()
}

function startNewConversation() {
  activeConversationId.value = null
  prompt.value = ''
  referenceImages.value = []
  referenceFiles.value = []
}

function removeConversation(id: string) {
  conversations.value = deleteImageConversation(id)
  if (activeConversationId.value === id) {
    activeConversationId.value = conversations.value[0]?.id ?? null
  }
}

function clearHistory() {
  clearImageConversations()
  conversations.value = []
  activeConversationId.value = null
  deleteConfirm.value = null
}

function openDeleteConversationConfirm(id: string) {
  deleteConfirm.value = { type: 'one', conversationId: id }
}

function openClearHistoryConfirm() {
  deleteConfirm.value = { type: 'all' }
}

function openDeletePromptConfirm(conversationID: string, turnID: string) {
  deleteConfirm.value = { type: 'prompt', conversationId: conversationID, turnId: turnID }
}

function openDeleteResultsConfirm(conversationID: string, turnID: string) {
  deleteConfirm.value = { type: 'results', conversationId: conversationID, turnId: turnID }
}

function confirmDelete() {
  const target = deleteConfirm.value
  if (!target) return
  if (target.type === 'all') {
    clearHistory()
    return
  }
  if (target.type === 'one') {
    removeConversation(target.conversationId)
  } else if (target.type === 'prompt') {
    patchConversationTurn(target.conversationId, target.turnId, { promptDeleted: true })
  } else if (target.type === 'results') {
    patchConversationTurn(target.conversationId, target.turnId, { resultsDeleted: true })
  }
  deleteConfirm.value = null
}

function patchConversationTurn(conversationID: string, turnID: string, patch: Partial<ImageTurn>) {
  updateTurn(conversationID, turnID, patch)
}

function startRenameConversation(conversation: ImageConversation, event: Event) {
  event.stopPropagation()
  editingConversationId.value = conversation.id
  editingConversationTitle.value = conversation.title
}

function commitRenameConversation() {
  const id = editingConversationId.value
  const title = editingConversationTitle.value.trim()
  if (id && title) {
    conversations.value = renameImageConversation(id, title)
  }
  editingConversationId.value = null
  editingConversationTitle.value = ''
}

function cancelRenameConversation() {
  editingConversationId.value = null
  editingConversationTitle.value = ''
}

function conversationStats(conversation: ImageConversation) {
  return conversation.turns.reduce(
    (stats, turn) => {
      if (turn.status === 'queued') stats.queued += 1
      if (turn.status === 'generating') stats.running += 1
      return stats
    },
    { queued: 0, running: 0 }
  )
}

function retryImage(turn: ImageTurn, imageID: string) {
  const key = selectedKey.value
  const conversationID = activeConversationId.value
  if (!key || !conversationID) return
  const retryID = `${turn.id}-${createImageConversationId()}`
  const files = turn.referenceImages.map((image, index) => dataUrlToFile(image.dataUrl, image.name || `reference-${index + 1}.png`, image.type))
  updateTurn(conversationID, turn.id, {
    error: undefined,
    images: turn.images.map((image) => (image.id === imageID ? { id: retryID, status: 'loading' as const } : image)),
  })
  recomputeTurnStatus(conversationID, turn.id)
  enqueueImageJobs([buildJob(conversationID, turn, retryID, key.key, files)])
}

function dismissTurnErrors(turn: ImageTurn) {
  const conversationID = activeConversationId.value
  if (!conversationID) return
  const images = turn.images.filter((image) => image.status !== 'error')
  updateTurn(conversationID, turn.id, { images, error: undefined })
  recomputeTurnStatus(conversationID, turn.id)
}

async function addReferenceFiles(files: File[]) {
  const images = files.filter(isImageFile).slice(0, 4 - referenceImages.value.length)
  if (images.length === 0) return
  try {
    const stored = await Promise.all(images.map(fileToStoredReferenceImage))
    referenceImages.value = [...referenceImages.value, ...stored]
    referenceFiles.value = [...referenceFiles.value, ...images]
  } catch {
    appStore.showError(t('images.readReferenceFailed'))
  }
}

function removeReference(index: number) {
  referenceImages.value.splice(index, 1)
  referenceFiles.value.splice(index, 1)
}

function onFileInput(event: Event) {
  const input = event.target as HTMLInputElement
  void addReferenceFiles(Array.from(input.files || []))
  input.value = ''
}

function onPaste(event: ClipboardEvent) {
  const files = Array.from(event.clipboardData?.files || []).filter(isImageFile)
  if (files.length === 0) return
  event.preventDefault()
  void addReferenceFiles(files)
}

function onDragEnter(event: DragEvent) {
  if (event.dataTransfer && hasImageFiles(event.dataTransfer)) {
    dragging.value = true
  }
}

function onDragOver(event: DragEvent) {
  if (event.dataTransfer && hasImageFiles(event.dataTransfer)) {
    event.dataTransfer.dropEffect = 'copy'
    dragging.value = true
  }
}

function onDragLeave(event: DragEvent) {
  const related = event.relatedTarget
  if (related instanceof Node && event.currentTarget instanceof Node && event.currentTarget.contains(related)) {
    return
  }
  dragging.value = false
}

function onDrop(event: DragEvent) {
  dragging.value = false
  void addReferenceFiles(Array.from(event.dataTransfer?.files || []))
}

function updateImageWidth(value: string) {
  imageWidth.value = sanitizeDimension(value, '1024')
  size.value = `${imageWidth.value}x${imageHeight.value}`
  imageRatio.value = ratioFromDimensions(imageWidth.value, imageHeight.value)
  imageTier.value = tierFromDimensions(imageWidth.value, imageHeight.value)
}

function updateImageHeight(value: string) {
  imageHeight.value = sanitizeDimension(value, '1024')
  size.value = `${imageWidth.value}x${imageHeight.value}`
  imageRatio.value = ratioFromDimensions(imageWidth.value, imageHeight.value)
  imageTier.value = tierFromDimensions(imageWidth.value, imageHeight.value)
}

function selectAspect(option: { ratio: string; tier: string; width: string; height: string; disabled?: boolean }) {
  if (option.disabled) return
  imageRatio.value = option.ratio
  imageTier.value = option.tier
  imageWidth.value = option.width
  imageHeight.value = option.height
  size.value = `${option.width}x${option.height}`
}

function setCount(value: number | string) {
  count.value = clampImageCount(Number(value))
}

function setImageSizeFromTurn(turn: ImageTurn) {
  const [width, height] = parseSize(turn.size)
  imageWidth.value = width
  imageHeight.value = height
  imageRatio.value = turn.ratio || ratioFromDimensions(width, height)
  imageTier.value = turn.tier || tierFromDimensions(width, height)
  size.value = `${width}x${height}`
}

function parseSize(value: string): [string, string] {
  const match = String(value || '').match(/^(\d+)x(\d+)$/)
  return match ? [match[1], match[2]] : ['1024', '1024']
}

function sanitizeDimension(value: string, fallback: string): string {
  const normalized = Math.max(1, Math.floor(Number(value) || Number(fallback)))
  return String(normalized)
}

function ratioFromDimensions(width: string, height: string): string {
  const w = Math.max(1, Math.floor(Number(width) || 1))
  const h = Math.max(1, Math.floor(Number(height) || 1))
  const divisor = gcd(w, h)
  return `${Math.floor(w / divisor)}:${Math.floor(h / divisor)}`
}

function tierFromDimensions(width: string, height: string): string {
  const longest = Math.max(Number(width) || 0, Number(height) || 0)
  if (longest >= 3840) return '4k'
  if (longest >= 2048) return '2k'
  return '1k'
}

function gcd(a: number, b: number): number {
  let x = Math.abs(a)
  let y = Math.abs(b)
  while (y) {
    const next = x % y
    x = y
    y = next
  }
  return x || 1
}

function isImageFile(file: File): boolean {
  return file.type.startsWith('image/') || /\.(avif|bmp|gif|heic|heif|ico|jpe?g|png|svg|tiff?|webp)$/i.test(file.name)
}

function hasImageFiles(dataTransfer: DataTransfer): boolean {
  const items = Array.from(dataTransfer.items || [])
  if (items.length > 0) {
    return items.some((item) => item.kind === 'file' && item.type.startsWith('image/'))
  }
  return Array.from(dataTransfer.files || []).some(isImageFile)
}

function fileToStoredReferenceImage(file: File): Promise<StoredReferenceImage> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => {
      resolve({
        name: file.name || 'reference.png',
        type: file.type || 'image/png',
        dataUrl: String(reader.result || ''),
      })
    }
    reader.onerror = () => reject(reader.error)
    reader.readAsDataURL(file)
  })
}

function dataUrlToFile(dataUrl: string, fileName: string, mimeType?: string): File {
  const [header, content] = dataUrl.split(',', 2)
  const matchedMimeType = header.match(/data:(.*?);base64/)?.[1]
  const binary = atob(content || '')
  const bytes = new Uint8Array(binary.length)
  for (let index = 0; index < binary.length; index += 1) {
    bytes[index] = binary.charCodeAt(index)
  }
  return new File([bytes], fileName, { type: mimeType || matchedMimeType || 'image/png' })
}

async function imageResponseItemToFile(image: ImageResponseItem, fileName: string): Promise<File> {
  if (image.b64_json) {
    return dataUrlToFile(`data:image/png;base64,${image.b64_json}`, fileName, 'image/png')
  }
  if (!image.url) {
    throw new Error('missing image url')
  }
  if (image.url.startsWith('data:')) {
    return dataUrlToFile(image.url, fileName)
  }
  const response = await fetch(image.url)
  if (!response.ok) {
    throw new Error(`failed to fetch image: ${response.status}`)
  }
  const blob = await response.blob()
  const type = blob.type || 'image/png'
  return new File([blob], fileName, { type })
}

function formatTime(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  return new Intl.DateTimeFormat(undefined, {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  }).format(date)
}

function focusPrompt() {
  textareaRef.value?.focus()
}

async function scrollToLatest() {
  await nextTick()
  const panel = resultPanelRef.value
  if (panel) {
    panel.scrollTop = panel.scrollHeight
  }
}

async function scrollToComposer() {
  await nextTick()
  const panel = resultPanelRef.value
  if (panel) {
    panel.scrollTop = panel.scrollHeight
  }
  focusPrompt()
}

function openReferenceImages(images: StoredReferenceImage[], prefix: string, index: number) {
  openLightbox(images.map((item, idx) => ({ id: `${prefix}-${idx}`, src: item.dataUrl })), index)
}

function openGeneratedImages(turn: ImageTurn, index: number) {
  const source = turn.images[index]
  const items: LightboxItem[] = turn.images
    .filter((item) => item.status === 'success' && imageSrc(item))
    .map((item) => ({
      id: item.id,
      src: imageSrc(item),
      sizeLabel: item.b64_json ? formatBase64ImageSize(item.b64_json) : undefined,
      dimensions: imageDimensions[item.id] || undefined,
    }))
  const lightboxIndex = Math.max(0, items.findIndex((item) => item.id === source?.id))
  openLightbox(items, lightboxIndex)
}

function openLightbox(items: LightboxItem[], index: number) {
  lightbox.items = items.filter((item) => item.src)
  lightbox.index = Math.min(Math.max(index, 0), Math.max(lightbox.items.length - 1, 0))
  lightbox.open = lightbox.items.length > 0
}

function extractErrorMessage(err: unknown, fallback: string): string {
  if (!err || typeof err !== 'object') return fallback
  const data = err as Record<string, any>
  return data.message || data.error?.message || data.detail || fallback
}
</script>

<style scoped>
.image-page-shell {
  min-height: calc(100dvh - 7.5rem);
}

.image-workspace {
  display: grid;
  min-height: calc(100dvh - 7.5rem);
  grid-template-columns: minmax(0, 1fr);
  gap: 0.5rem;
  overflow: hidden;
}

@media (min-width: 1024px) {
  .image-page-shell,
  .image-workspace {
    height: calc(100dvh - 7.5rem);
    min-height: 0;
  }

  .image-workspace {
    grid-template-columns: 240px minmax(0, 1fr);
  }
}

.image-state-panel {
  display: flex;
  min-height: calc(100dvh - 7.5rem);
  align-items: center;
  justify-content: center;
  border: 1px solid rgb(231 229 228 / 0.8);
  background: rgb(255 255 255 / 0.72);
  padding: 2rem;
}

:global(.dark) .image-state-panel {
  border-color: rgb(255 255 255 / 0.1);
  background: rgb(12 10 9 / 0.72);
}

.image-sidebar {
  min-height: 0;
  flex-direction: column;
  gap: 0.75rem;
  overflow: hidden;
  border-right: 1px solid rgb(231 229 228 / 0.7);
  padding-right: 0.75rem;
}

:global(.dark) .image-sidebar {
  border-color: rgb(255 255 255 / 0.1);
}

.image-main {
  display: flex;
  min-height: 0;
  flex-direction: column;
  gap: 0.5rem;
  overflow: hidden;
}

@media (min-width: 640px) {
  .image-main {
    gap: 1rem;
  }
}

.image-history-list {
  min-height: 0;
  flex: 1 1 0%;
  overflow-y: auto;
  padding-right: 0.25rem;
  scrollbar-color: rgb(120 113 108 / 0.45) transparent;
  scrollbar-width: thin;
}

.image-history-list::-webkit-scrollbar {
  width: 0.375rem;
}

.image-history-list::-webkit-scrollbar-thumb {
  border-radius: 9999px;
  background: rgb(168 162 158 / 0.45);
}

.image-results-scroll {
  height: 100%;
  min-height: 0;
  overflow-y: auto;
  overscroll-behavior: contain;
  scroll-behavior: smooth;
  padding: 0.5rem 0.25rem;
}

@media (min-width: 640px) {
  .image-results-scroll {
    padding: 1rem;
  }
}

.image-content-width {
  margin-inline: auto;
  width: min(980px, calc(100vw - 2rem));
  max-width: 100%;
}

.image-empty-title {
  font-family: "Palatino Linotype", "Book Antiqua", "URW Palladio L", "Times New Roman", serif;
  font-size: clamp(1.75rem, 5vw, 3rem);
  font-weight: 600;
  line-height: 1.12;
  color: rgb(12 10 9);
}

:global(.dark) .image-empty-title {
  color: rgb(250 250 249);
}

.image-empty-subtitle {
  font-family: "Palatino Linotype", "Book Antiqua", "URW Palladio L", "Times New Roman", serif;
  letter-spacing: 0.01em;
}

.image-composer-wrap {
  flex-shrink: 0;
  margin-inline: auto;
  width: min(980px, calc(100vw - 2rem));
  max-width: 100%;
  padding-inline: 0.25rem;
}

@media (min-width: 640px) {
  .image-composer-wrap {
    padding-inline: 0;
  }
}

.image-composer-shell {
  position: relative;
  overflow: hidden;
  border: 1px solid rgb(231 229 228);
  border-radius: 1.5rem;
  background: rgb(255 255 255);
  box-shadow: 0 14px 60px -42px rgb(15 23 42 / 0.45);
  transition:
    background-color 200ms ease,
    border-color 200ms ease,
    box-shadow 200ms ease;
}

@media (min-width: 640px) {
  .image-composer-shell {
    border-radius: 2rem;
    box-shadow: none;
  }
}

:global(.dark) .image-composer-shell {
  border-color: rgb(255 255 255 / 0.1);
  background: rgb(12 10 9 / 0.86);
}

.image-composer-dragging {
  border-color: rgb(28 25 23);
  background: rgb(250 250 249);
}

:global(.dark) .image-composer-dragging {
  border-color: rgb(255 255 255 / 0.6);
  background: rgb(28 25 23);
}

.image-prompt-input {
  min-height: 82px;
  width: 100%;
  resize: none;
  border: 0;
  border-radius: 1.5rem;
  background: transparent;
  padding: 1rem 1rem 0.5rem;
  font-size: 15px;
  line-height: 1.5rem;
  color: rgb(28 25 23);
  outline: none;
  box-shadow: none;
}

.image-prompt-input::placeholder {
  color: rgb(168 162 158);
}

@media (min-width: 640px) {
  .image-prompt-input {
    min-height: 148px;
    border-radius: 2rem;
    padding: 1.5rem 1.5rem 5.25rem;
    line-height: 1.75rem;
  }
}

@media (min-width: 1280px) {
  .image-prompt-input {
    min-height: 148px;
  }
}

:global(.dark) .image-prompt-input {
  color: rgb(245 245 244);
}

:global(.dark) .image-prompt-input::placeholder {
  color: rgb(120 113 108);
}

.image-drop-overlay {
  pointer-events: none;
  position: absolute;
  inset: 0;
  z-index: 20;
  display: flex;
  align-items: center;
  justify-content: center;
  border: 2px dashed rgb(28 25 23);
  border-radius: 1.5rem;
  background: rgb(255 255 255 / 0.86);
  font-size: 0.875rem;
  font-weight: 500;
  backdrop-filter: blur(1px);
}

@media (min-width: 640px) {
  .image-drop-overlay {
    border-radius: 2rem;
  }
}

:global(.dark) .image-drop-overlay {
  border-color: rgb(255 255 255 / 0.75);
  background: rgb(12 10 9 / 0.75);
}

.image-composer-controls {
  border-top: 1px solid rgb(245 245 244);
  background: rgb(255 255 255);
  padding: 0.5rem 0.75rem 0.75rem;
}

@media (min-width: 640px) {
  .image-composer-controls {
    position: absolute;
    inset-inline: 0;
    bottom: 0;
    border-top: 0;
    background: linear-gradient(to top, rgb(255 255 255), rgb(255 255 255 / 0.95), rgb(255 255 255 / 0));
    padding: 1.5rem 1.5rem 1rem;
  }
}

:global(.dark) .image-composer-controls {
  border-color: rgb(255 255 255 / 0.1);
  background: rgb(12 10 9 / 0.96);
}

@media (min-width: 640px) {
  :global(.dark) .image-composer-controls {
    background: linear-gradient(to top, rgb(12 10 9), rgb(12 10 9 / 0.95), rgb(12 10 9 / 0));
  }
}

.image-control-row {
  display: flex;
  min-width: 0;
  flex: 1 1 0%;
  flex-wrap: nowrap;
  align-items: center;
  gap: 0.375rem;
  overflow-x: auto;
  padding-bottom: 0.125rem;
  scrollbar-width: none;
}

.image-control-row::-webkit-scrollbar {
  display: none;
}

@media (min-width: 640px) {
  .image-control-row {
    flex-wrap: wrap;
    gap: 0.75rem;
    overflow: visible;
    padding-bottom: 0;
  }
}

.image-composer-controls {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 0.5rem;
}

@media (min-width: 640px) {
  .image-composer-controls {
    gap: 0.75rem;
  }
}

.image-primary-button,
.image-soft-button,
.image-danger-button,
.image-icon-button,
.image-pill-button,
.image-settings-button,
.image-key-chip,
.image-send-button,
.image-mini-button,
.image-mini-icon-button,
.image-result-action {
  display: inline-flex;
  cursor: pointer;
  align-items: center;
  justify-content: center;
  gap: 0.5rem;
  font-weight: 500;
  transition:
    background-color 180ms ease,
    border-color 180ms ease,
    color 180ms ease,
    opacity 180ms ease,
    transform 180ms ease;
}

.image-primary-button {
  border-radius: 9999px;
  background: rgb(12 10 9);
  padding: 0.625rem 1rem;
  font-size: 0.875rem;
  color: white;
}

.image-primary-button:hover {
  background: rgb(41 37 36);
}

:global(.dark) .image-primary-button {
  background: white;
  color: rgb(12 10 9);
}

:global(.dark) .image-primary-button:hover {
  background: rgb(231 229 228);
}

.image-soft-button,
.image-danger-button,
.image-icon-button,
.image-pill-button,
.image-settings-button,
.image-key-chip,
.image-mini-button,
.image-mini-icon-button,
.image-result-action {
  border: 1px solid rgb(231 229 228);
  background: rgb(255 255 255 / 0.9);
  color: rgb(68 64 60);
}

.image-soft-button {
  border-radius: 9999px;
  padding: 0.5625rem 0.875rem;
  font-size: 0.875rem;
  box-shadow: 0 1px 2px rgb(15 23 42 / 0.04);
}

.image-danger-button {
  border-radius: 9999px;
  border-color: rgb(225 29 72);
  background: rgb(225 29 72);
  padding: 0.5625rem 0.875rem;
  font-size: 0.875rem;
  color: white;
}

.image-danger-button:hover {
  border-color: rgb(190 18 60);
  background: rgb(190 18 60);
}

.image-icon-button {
  padding: 0;
}

.image-pill-button {
  height: 2.25rem;
  flex-shrink: 0;
  border-radius: 9999px;
  padding: 0 0.75rem;
  font-size: 0.75rem;
}

.image-settings-anchor {
  position: relative;
  min-width: 0;
  flex-shrink: 0;
}

.image-settings-button,
.image-key-chip {
  height: 2.25rem;
  min-width: 0;
  flex-shrink: 0;
  border-radius: 9999px;
  background: rgb(245 245 244);
  padding: 0 0.75rem;
  font-size: 0.75rem;
  font-weight: 600;
  color: rgb(28 25 23);
}

.image-key-chip {
  max-width: min(14rem, 42vw);
}

.image-settings-button {
  max-width: min(18rem, 52vw);
  justify-content: space-between;
}

@media (min-width: 640px) {
  .image-settings-button,
  .image-key-chip {
    height: 2.5rem;
    padding-inline: 1rem;
    font-size: 0.875rem;
  }
}

@media (min-width: 640px) {
  .image-pill-button {
    height: 2.5rem;
    padding-inline: 1rem;
    font-size: 0.875rem;
  }
}

.image-soft-button:hover,
.image-icon-button:hover,
.image-pill-button:hover,
.image-settings-button:hover,
.image-mini-button:hover,
.image-mini-icon-button:hover,
.image-result-action:hover {
  border-color: rgb(214 211 209);
  background: white;
  color: rgb(28 25 23);
}

:global(.dark) .image-soft-button,
:global(.dark) .image-icon-button,
:global(.dark) .image-pill-button,
:global(.dark) .image-settings-button,
:global(.dark) .image-key-chip,
:global(.dark) .image-mini-button,
:global(.dark) .image-mini-icon-button,
:global(.dark) .image-result-action {
  border-color: rgb(255 255 255 / 0.1);
  background: rgb(28 25 23 / 0.85);
  color: rgb(231 229 228);
}

:global(.dark) .image-soft-button:hover,
:global(.dark) .image-icon-button:hover,
:global(.dark) .image-pill-button:hover,
:global(.dark) .image-settings-button:hover,
:global(.dark) .image-mini-button:hover,
:global(.dark) .image-mini-icon-button:hover,
:global(.dark) .image-result-action:hover {
  border-color: rgb(255 255 255 / 0.18);
  background: rgb(41 37 36);
  color: white;
}

.image-mini-button {
  border-radius: 9999px;
  padding: 0.25rem 0.625rem;
  font-size: 11px;
}

.image-mini-icon-button {
  height: 1.5rem;
  width: 1.5rem;
  border-radius: 9999px;
  padding: 0;
  color: rgb(168 162 158);
}

.image-result-action {
  height: 1.75rem;
  min-width: 1.75rem;
  border-radius: 9999px;
  padding-inline: 0.5rem;
  font-size: 10px;
}

@media (min-width: 640px) {
  .image-result-action {
    height: 2rem;
    padding-inline: 0.75rem;
    font-size: 0.75rem;
  }
}

.image-send-button {
  height: 2.5rem;
  width: 2.5rem;
  flex-shrink: 0;
  border-radius: 9999px;
  background: rgb(12 10 9);
  color: white;
}

@media (min-width: 640px) {
  .image-send-button {
    height: 2.75rem;
    width: 2.75rem;
  }
}

.image-send-button:hover {
  background: rgb(41 37 36);
}

:global(.dark) .image-send-button {
  background: white;
  color: rgb(12 10 9);
}

.image-primary-button:disabled,
.image-soft-button:disabled,
.image-icon-button:disabled,
.image-pill-button:disabled,
.image-settings-button:disabled,
.image-send-button:disabled,
.image-mini-button:disabled,
.image-mini-icon-button:disabled,
.image-result-action:disabled {
  cursor: not-allowed;
  opacity: 0.45;
  transform: none;
}

.image-task-pill,
.image-meta-pill {
  display: inline-flex;
  align-items: center;
  gap: 0.375rem;
  border-radius: 9999px;
  background: rgb(245 245 244);
  padding: 0.25rem 0.75rem;
  font-weight: 500;
}

.image-settings-panel {
  position: fixed;
  z-index: 100000015;
  max-height: min(62dvh, 720px);
  overflow-y: auto;
  border: 1px solid rgb(231 229 228 / 0.8);
  border-radius: 1.5rem;
  background: white;
  padding: 1rem;
  box-shadow: 0 30px 90px -34px rgb(15 23 42 / 0.42);
  transform: translateY(-100%);
}

@media (min-width: 640px) {
  .image-settings-panel {
    max-height: none;
    overflow: visible;
    padding: 1.25rem;
  }
}

:global(.dark) .image-settings-panel {
  border-color: rgb(255 255 255 / 0.1);
  background: rgb(12 10 9);
}

.image-choice-button,
.image-aspect-button,
.image-count-input {
  border: 1px solid rgb(231 229 228);
  background: white;
  color: rgb(41 37 36);
  transition:
    border-color 180ms ease,
    background-color 180ms ease,
    color 180ms ease,
    opacity 180ms ease;
}

.image-choice-button {
  height: 2.25rem;
  border-radius: 9999px;
  font-size: 0.875rem;
}

.image-aspect-button {
  display: flex;
  height: 4rem;
  cursor: pointer;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 0.25rem;
  border-radius: 1rem;
  font-size: 0.875rem;
}

.image-count-input {
  height: 2.25rem;
  min-width: 0;
  border-radius: 9999px;
  padding-inline: 0.75rem;
  text-align: center;
  font-size: 0.875rem;
  font-weight: 500;
  outline: none;
}

.image-choice-button:hover,
.image-aspect-button:hover,
.image-count-input:focus {
  border-color: rgb(214 211 209);
  background: rgb(250 250 249);
}

.image-choice-button-active {
  border-color: rgb(12 10 9);
  color: rgb(12 10 9);
  font-weight: 600;
}

.image-aspect-button-disabled {
  cursor: not-allowed;
  border-color: rgb(245 245 244);
  background: rgb(250 250 249);
  color: rgb(214 211 209);
}

:global(.dark) .image-choice-button,
:global(.dark) .image-aspect-button,
:global(.dark) .image-count-input {
  border-color: rgb(255 255 255 / 0.1);
  background: rgb(28 25 23);
  color: rgb(231 229 228);
}

:global(.dark) .image-choice-button:hover,
:global(.dark) .image-aspect-button:hover,
:global(.dark) .image-count-input:focus {
  border-color: rgb(255 255 255 / 0.18);
  background: rgb(41 37 36);
}

:global(.dark) .image-choice-button-active {
  border-color: white;
  color: white;
}

:global(.dark) .image-aspect-button-disabled {
  border-color: rgb(255 255 255 / 0.08);
  background: rgb(28 25 23 / 0.6);
  color: rgb(120 113 108);
}

.image-dimension-field {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  border-radius: 0.75rem;
  background: rgb(245 245 244);
  padding: 0.375rem 0.75rem;
  color: rgb(120 113 108);
}

.image-dimension-field input {
  min-width: 0;
  width: 100%;
  border: 0;
  background: transparent;
  font-size: 0.875rem;
  font-weight: 600;
  color: rgb(41 37 36);
  outline: none;
}

:global(.dark) .image-dimension-field {
  background: rgb(41 37 36);
  color: rgb(168 162 158);
}

:global(.dark) .image-dimension-field input {
  color: rgb(250 250 249);
}

.image-task-pill {
  flex-shrink: 0;
  background: rgb(255 251 235);
  font-size: 10px;
  color: rgb(180 83 9);
}

:global(.dark) .image-task-pill,
:global(.dark) .image-meta-pill {
  background: rgb(41 37 36);
}

:global(.dark) .image-task-pill {
  color: rgb(253 230 138);
}

.image-generating-progress {
  animation: image-generating-progress 1.45s ease-in-out infinite;
}

.image-generating-pulse {
  animation: image-generating-pulse 1.8s ease-out infinite;
}

.image-generating-shimmer {
  background: linear-gradient(110deg, transparent 0%, rgba(255, 255, 255, 0.58) 45%, transparent 70%);
  transform: translateX(-100%);
  animation: image-generating-shimmer 1.7s ease-in-out infinite;
}

:global(.dark) .image-generating-shimmer {
  background: linear-gradient(110deg, transparent 0%, rgba(255, 255, 255, 0.08) 45%, transparent 70%);
}

@keyframes image-generating-progress {
  0% {
    transform: translateX(-120%);
  }
  50% {
    transform: translateX(140%);
  }
  100% {
    transform: translateX(360%);
  }
}

@keyframes image-generating-pulse {
  0% {
    opacity: 0.75;
    transform: scale(1);
  }
  100% {
    opacity: 0;
    transform: scale(1.55);
  }
}

@keyframes image-generating-shimmer {
  100% {
    transform: translateX(100%);
  }
}

@media (prefers-reduced-motion: reduce) {
  .image-primary-button,
  .image-soft-button,
  .image-icon-button,
  .image-pill-button,
  .image-send-button,
  .image-mini-button,
  .image-result-action,
  .image-composer-shell,
  .image-generating-progress,
  .image-generating-pulse,
  .image-generating-shimmer {
    animation: none;
    transition-duration: 1ms;
  }
}
</style>
