export type Lang = 'en' | 'ru';

const translations: Record<string, Record<Lang, string>> = {
  // Sidebar
  'sidebar.home': { en: 'Home', ru: 'Главная' },
  'sidebar.scan': { en: 'Scan', ru: 'Сканирование' },
  'sidebar.tools': { en: 'Tools', ru: 'Инструменты' },
  'sidebar.jobs': { en: 'Jobs', ru: 'Задачи' },
  'sidebar.settings': { en: 'Settings', ru: 'Настройки' },
  'sidebar.expand': { en: 'Expand', ru: 'Развернуть' },
  'sidebar.collapse': { en: 'Collapse', ru: 'Свернуть' },

  // Header
  'header.update': { en: 'Update', ru: 'Обновление' },
  'header.upToDate': { en: 'Up to date', ru: 'Актуальная версия' },
  'header.updateAvailable': { en: 'Update available', ru: 'Доступно обновление' },
  'header.checking': { en: 'Checking...', ru: 'Проверка...' },
  'header.offline': { en: 'Offline', ru: 'Нет сети' },
  'header.updateApp': { en: 'Update to', ru: 'Обновить до' },
  'header.toolUpdates': { en: 'updates', ru: 'обновлений' },

  // HomePage
  'home.title': { en: 'Binary Decompiler', ru: 'Декомпилятор бинарных файлов' },
  'home.subtitle': { en: 'Automated decompilation pipeline for .NET, Delphi, and native targets', ru: 'Автоматический Pipeline декомпиляции для .NET, Delphi и нативных целей' },
  'home.toolsInstalled': { en: 'Tools installed', ru: 'Установлено инструментов' },
  'home.lastRun': { en: 'Last run', ru: 'Последний запуск' },
  'home.recipesAvailable': { en: 'Recipes available', ru: 'Доступно рецептов' },

  // DropZone
  'dropzone.text': { en: 'Drop folder or binary here', ru: 'Перетащите папку или файл сюда' },
  'dropzone.hint': { en: 'or click to select', ru: 'или нажмите для выбора' },

  // ScanPage
  'scan.title': { en: 'Scan Results', ru: 'Результаты сканирования' },
  'scan.scanning': { en: 'Scanning directory...', ru: 'Сканирование каталога...' },
  'scan.classifying': { en: 'Classifying binaries...', ru: 'Классификация файлов...' },
  'scan.targetsSelected': { en: 'targets selected', ru: 'целей выбрано' },
  'scan.startPipeline': { en: 'Start Pipeline', ru: 'Запустить Pipeline' },

  // FileTree
  'filetree.selected': { en: 'selected', ru: 'выбрано' },
  'filetree.all': { en: 'All', ru: 'Все' },
  'filetree.none': { en: 'None', ru: 'Сброс' },

  // ToolsPage
  'tools.title': { en: 'Tools', ru: 'Инструменты' },
  'tools.installAllMissing': { en: 'Install All Missing', ru: 'Установить все недостающие' },
  'tools.installing': { en: 'Installing...', ru: 'Установка...' },
  'tools.allInstalled': { en: 'All tools installed', ru: 'Все инструменты установлены' },
  'tools.loading': { en: 'Loading tools...', ru: 'Загрузка инструментов...' },
  'tools.empty': { en: 'No tools registered', ru: 'Нет зарегистрированных инструментов' },
  'tools.install': { en: 'Install', ru: 'Установить' },
  'tools.installed': { en: 'Installed', ru: 'Установлен' },

  // JobsPage
  'jobs.title': { en: 'Jobs', ru: 'Задачи' },
  'jobs.active': { en: 'Active', ru: 'Активно' },
  'jobs.noActive': { en: 'No active pipeline', ru: 'Нет активного Pipeline' },
  'jobs.history': { en: 'History', ru: 'История' },
  'jobs.cancel': { en: 'Cancel', ru: 'Отмена' },
  'jobs.step': { en: 'Step', ru: 'Шаг' },

  // SettingsPage
  'settings.title': { en: 'Settings', ru: 'Настройки' },
  'settings.loading': { en: 'Loading configuration...', ru: 'Загрузка конфигурации...' },
  'settings.saving': { en: 'Saving...', ru: 'Сохранение...' },
  'settings.language': { en: 'Language', ru: 'Язык' },

  // Settings — Updates
  'settings.updates': { en: 'Updates', ru: 'Обновления' },
  'settings.autoUpdateCheck': { en: 'Auto-check updates', ru: 'Автопроверка обновлений' },
  'settings.autoUpdateApp': { en: 'Auto-update application', ru: 'Автообновление приложения' },
  'settings.autoUpdateTools': { en: 'Auto-update tools', ru: 'Автообновление инструментов' },

  // Settings — Decompilation
  'settings.decompilation': { en: 'Decompilation', ru: 'Декомпиляция' },
  'settings.decompileProject': { en: 'Project mode (.csproj)', ru: 'Проектный режим (.csproj)' },
  'settings.generatePdb': { en: 'Generate PDB', ru: 'Генерировать PDB' },
  'settings.csharpVersion': { en: 'C# language version', ru: 'Версия C#' },
  'settings.keepIntermediates': { en: 'Keep intermediate files', ru: 'Сохранять промежуточные файлы' },
  'settings.skipSystemLibs': { en: 'Skip system libraries', ru: 'Пропускать системные библиотеки' },
  'settings.stopOnFirstError': { en: 'Stop on first error', ru: 'Останавливаться при ошибке' },
  'settings.maxFileSize': { en: 'Max file size (MB)', ru: 'Макс. размер файла (МБ)' },
  'settings.stepTimeout': { en: 'Step timeout (min)', ru: 'Таймаут шага (мин)' },
  'settings.outputDir': { en: 'Output directory', ru: 'Папка вывода' },

  // Settings — Security
  'settings.security': { en: 'Security', ru: 'Безопасность' },
  'settings.allowDynamicExecution': { en: 'Allow dynamic execution', ru: 'Разрешить динамическое выполнение' },
  'settings.sandboxWarning': { en: 'Sandbox warning', ru: 'Предупреждение о песочнице' },

  // Settings — Logging
  'settings.logging': { en: 'Logging', ru: 'Логирование' },
  'settings.logLevel': { en: 'Log level', ru: 'Уровень логов' },
  'settings.logToFile': { en: 'Log to file', ru: 'Запись в файл' },

  // StatusBar
  'status.ready': { en: 'Ready', ru: 'Готов' },
  'status.running': { en: 'Pipeline running', ru: 'Pipeline выполняется' },
  'status.complete': { en: 'Complete', ru: 'Завершено' },

  // Toast
  'toast.success': { en: 'Success', ru: 'Успешно' },
  'toast.error': { en: 'Error', ru: 'Ошибка' },
  'toast.info': { en: 'Info', ru: 'Информация' },

  // LogViewer
  'log.empty': { en: 'No log entries', ru: 'Нет записей в журнале' },

  // Pipeline view
  'pipeline.scanning': { en: 'Scanning...', ru: 'Сканирование...' },
  'pipeline.foundBinaries': { en: 'binaries found', ru: 'бинарников найдено' },
  'pipeline.recon': { en: 'Analyzing...', ru: 'Анализ...' },
  'pipeline.checkingTools': { en: 'Checking tools...', ru: 'Проверка инструментов...' },
  'pipeline.allToolsReady': { en: 'All tools ready', ru: 'Все инструменты готовы' },
  'pipeline.missingTools': { en: 'Missing tools', ru: 'Недостающие инструменты' },
  'pipeline.installMissing': { en: 'Install missing', ru: 'Установить недостающие' },
  'pipeline.executing': { en: 'Decompiling...', ru: 'Декомпиляция...' },
  'pipeline.step': { en: 'Step', ru: 'Шаг' },
  'pipeline.done': { en: 'Done', ru: 'Готово' },
  'pipeline.outputPath': { en: 'Output', ru: 'Результат' },
  'pipeline.filesDecompiled': { en: 'files decompiled', ru: 'файлов декомпилировано' },
  'pipeline.totalTime': { en: 'Total time', ru: 'Общее время' },
  'pipeline.error': { en: 'Error', ru: 'Ошибка' },

  // Home — pipeline inline
  'home.processing': { en: 'Processing', ru: 'Обработка' },
  'home.newFile': { en: 'New file', ru: 'Новый файл' },
  'home.done': { en: 'Done!', ru: 'Готово!' },
  'home.result': { en: 'Result', ru: 'Результат' },
  'home.openResult': { en: 'Open folder', ru: 'Открыть папку' },
  'home.analyzing': { en: 'Analyzing...', ru: 'Анализ...' },
  'home.preparingTools': { en: 'Preparing tools...', ru: 'Подготовка инструментов...' },
  'home.decompiling': { en: 'Decompiling...', ru: 'Декомпиляция...' },
  'home.error': { en: 'Error', ru: 'Ошибка' },

  // Home — open buttons
  'home.openFolder': { en: 'Open folder', ru: 'Открыть папку' },
  'home.openFile': { en: 'Open file', ru: 'Открыть файл' },

  // Tools page enhanced
  'tools.version': { en: 'Version', ru: 'Версия' },
  'tools.latest': { en: 'Latest', ru: 'Последняя' },
  'tools.updateAvailable': { en: 'Update available', ru: 'Доступно обновление' },
  'tools.update': { en: 'Update', ru: 'Обновить' },
  'tools.updateAll': { en: 'Update all', ru: 'Обновить все' },
  'tools.downloadAll': { en: 'Download all', ru: 'Скачать все' },
  'tools.delete': { en: 'Delete', ru: 'Удалить' },
  'tools.download': { en: 'Download', ru: 'Скачать' },
  'tools.notInstalled': { en: 'Not installed', ru: 'Не установлен' },
  'tools.available': { en: 'available', ru: 'доступна' },
  'tools.upToDate': { en: 'Up to date', ru: 'Актуален' },
  'tools.checking': { en: 'Checking updates...', ru: 'Проверка обновлений...' },

  // StatusBar enhanced
  'status.downloading': { en: 'Downloading', ru: 'Скачивание' },
  'status.installing': { en: 'Installing', ru: 'Установка' },

  // Operations footer
  'ops.title': { en: 'Operations', ru: 'Операции' },
  'ops.clear': { en: 'Clear', ru: 'Очистить' },

  // Settings — folder picker
  'settings.browse': { en: 'Browse...', ru: 'Обзор...' },
  'settings.githubToken': { en: 'GitHub Token (optional)', ru: 'GitHub Token (опционально)' },
  'settings.githubTokenHint': { en: 'Increases API limit from 60 to 5000 requests/hour', ru: 'Увеличивает лимит API с 60 до 5000 запросов/час' },
  'settings.createToken': { en: 'Create token', ru: 'Создать токен' },

  // Sidebar — pipeline
  'sidebar.pipeline': { en: 'Pipeline', ru: 'Pipeline' },

  // Runtimes
  'runtimes.title': { en: 'Runtimes', ru: 'Рантаймы' },
  'runtimes.dotnet': { en: '.NET SDK', ru: '.NET SDK' },
  'runtimes.java': { en: 'Java JRE', ru: 'Java JRE' },
  'runtimes.available': { en: 'Available', ru: 'Доступен' },
  'runtimes.missing': { en: 'Not installed', ru: 'Не установлен' },
  'runtimes.local': { en: 'Portable', ru: 'Портативный' },
  'runtimes.system': { en: 'System', ru: 'Системный' },
  'runtimes.install': { en: 'Download', ru: 'Скачать' },
  'runtimes.installing': { en: 'Downloading...', ru: 'Скачивание...' },
  'runtimes.required': { en: 'Required', ru: 'Необходим' },
  'runtimes.optional': { en: 'Optional', ru: 'Опциональный' },
  'runtimes.version': { en: 'Version', ru: 'Версия' },
  'runtimes.missingForPipeline': { en: 'Required runtimes are missing. Go to Tools to install.', ru: 'Необходимые рантаймы отсутствуют. Перейдите в Инструменты для установки.' },
};

export function t(lang: Lang, key: string): string {
  const entry = translations[key];
  if (!entry) return key;
  return entry[lang] ?? entry['en'] ?? key;
}

export function detectLang(): Lang {
  try {
    const saved = localStorage.getItem('morgue-lang');
    if (saved === 'ru' || saved === 'en') return saved;
    if (typeof navigator !== 'undefined' && navigator.language?.startsWith('ru')) return 'ru';
  } catch {}
  return 'en';
}
