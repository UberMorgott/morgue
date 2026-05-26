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

  // Settings — Pipeline
  'settings.pipeline': { en: 'Pipeline', ru: 'Pipeline' },
  'settings.outputDir': { en: 'Default output directory', ru: 'Каталог вывода по умолчанию' },
  'settings.stepTimeout': { en: 'Step timeout (minutes)', ru: 'Таймаут шага (минуты)' },
  'settings.concurrentTargets': { en: 'Concurrent targets', ru: 'Параллельных целей' },
  'settings.stopOnFirstError': { en: 'Stop on first error', ru: 'Остановить при первой ошибке' },
  'settings.keepIntermediates': { en: 'Keep intermediate files', ru: 'Сохранять промежуточные файлы' },

  // Settings — Skip List
  'settings.skipList': { en: 'Skip List', ru: 'Список исключений' },
  'settings.skipSystemLibs': { en: 'Skip system libraries', ru: 'Пропускать системные библиотеки' },

  // Settings — Decompiler
  'settings.decompiler': { en: 'Decompiler', ru: 'Декомпилятор' },
  'settings.csharpVersion': { en: 'C# language version', ru: 'Версия языка C#' },
  'settings.generatePdb': { en: 'Generate PDB files', ru: 'Генерировать PDB файлы' },
  'settings.decompileProject': { en: 'Decompile as project', ru: 'Декомпилировать как проект' },
  'settings.generateCallgraph': { en: 'Generate call graph', ru: 'Генерировать граф вызовов' },

  // Settings — Network
  'settings.network': { en: 'Network', ru: 'Сеть' },
  'settings.githubToken': { en: 'GitHub token', ru: 'GitHub токен' },
  'settings.downloadRetries': { en: 'Download retries', ru: 'Повторов загрузки' },
  'settings.autoUpdateCheck': { en: 'Auto-check for updates', ru: 'Авто-проверка обновлений' },

  // Settings — Logging
  'settings.logging': { en: 'Logging', ru: 'Логирование' },
  'settings.logLevel': { en: 'Log level', ru: 'Уровень логов' },
  'settings.logToFile': { en: 'Log to file', ru: 'Писать логи в файл' },

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
  'tools.upToDate': { en: 'Up to date', ru: 'Актуален' },
  'tools.checking': { en: 'Checking updates...', ru: 'Проверка обновлений...' },

  // StatusBar enhanced
  'status.downloading': { en: 'Downloading', ru: 'Скачивание' },
  'status.installing': { en: 'Installing', ru: 'Установка' },

  // Settings — folder picker
  'settings.browse': { en: 'Browse...', ru: 'Обзор...' },

  // Sidebar — pipeline
  'sidebar.pipeline': { en: 'Pipeline', ru: 'Pipeline' },
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
