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
  'dropzone.pickFile': { en: 'File', ru: 'Файл' },
  'dropzone.pickDir': { en: 'Folder', ru: 'Папка' },

  // ScanPage
  'scan.title': { en: 'Scan Results', ru: 'Результаты сканирования' },
  'scan.scanning': { en: 'Scanning directory...', ru: 'Сканирование каталога...' },
  'scan.classifying': { en: 'Classifying binaries...', ru: 'Классификация файлов...' },
  'scan.targetsSelected': { en: 'targets selected', ru: 'целей выбрано' },
  'scan.startPipeline': { en: 'Start Pipeline', ru: 'Запустить Pipeline' },

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
  'settings.autoUpdateCheckHint': { en: 'Periodically check for new versions of Morgue', ru: 'Периодически проверять наличие новых версий Morgue' },
  'settings.autoUpdateApp': { en: 'Auto-update application', ru: 'Автообновление приложения' },
  'settings.autoUpdateAppHint': { en: 'Download and install app updates automatically', ru: 'Автоматически скачивать и устанавливать обновления приложения' },
  'settings.autoUpdateTools': { en: 'Auto-update tools', ru: 'Автообновление инструментов' },
  'settings.autoUpdateToolsHint': { en: 'Keep decompilation tools up to date automatically', ru: 'Автоматически обновлять инструменты декомпиляции' },

  // Settings — Decompilation
  'settings.decompilation': { en: 'Decompilation', ru: 'Декомпиляция' },
  'settings.decompileProject': { en: 'Project mode (.csproj)', ru: 'Проектный режим (.csproj)' },
  'settings.decompileProjectHint': { en: 'Output as Visual Studio project instead of loose files', ru: 'Выводить как проект Visual Studio вместо отдельных файлов' },
  'settings.generatePdb': { en: 'Generate PDB', ru: 'Генерировать PDB' },
  'settings.generatePdbHint': { en: 'Create debug symbols for decompiled assemblies', ru: 'Создавать отладочные символы для декомпилированных сборок' },
  'settings.csharpVersion': { en: 'C# language version', ru: 'Версия C#' },
  'settings.keepIntermediates': { en: 'Keep intermediate files', ru: 'Сохранять промежуточные файлы' },
  'settings.keepIntermediatesHint': { en: 'Preserve temp files from each pipeline step for debugging', ru: 'Сохранять временные файлы каждого шага для отладки' },
  'settings.skipSystemLibs': { en: 'Skip system libraries', ru: 'Пропускать системные библиотеки' },
  'settings.skipSystemLibsHint': { en: 'Skip .NET framework and standard library DLLs', ru: 'Пропускать DLL-библиотеки .NET Framework и стандартной библиотеки' },
  'settings.stopOnFirstError': { en: 'Stop on first error', ru: 'Останавливаться при ошибке' },
  'settings.stopOnFirstErrorHint': { en: 'Abort pipeline immediately when any target fails', ru: 'Прервать pipeline при первой ошибке в любом файле' },
  'settings.maxFileSize': { en: 'Max file size (MB)', ru: 'Макс. размер файла (МБ)' },
  'settings.stepTimeout': { en: 'Step timeout (min)', ru: 'Таймаут шага (мин)' },
  'settings.outputDir': { en: 'Output directory', ru: 'Папка вывода' },

  // Settings — Security
  'settings.security': { en: 'Security', ru: 'Безопасность' },
  'settings.allowDynamicExecution': { en: 'Allow dynamic execution', ru: 'Разрешить динамическое выполнение' },
  'settings.allowDynamicExecutionHint': { en: 'Allow tools to execute code during decompilation (required by some deobfuscators)', ru: 'Разрешить инструментам выполнять код при декомпиляции (необходимо для некоторых деобфускаторов)' },
  'settings.sandboxWarning': { en: 'Sandbox warning', ru: 'Предупреждение о песочнице' },
  'settings.sandboxWarningHint': { en: 'Show warning before running untrusted binaries', ru: 'Показывать предупреждение перед запуском недоверенных бинарников' },

  // Settings — Logging
  'settings.logging': { en: 'Logging', ru: 'Логирование' },
  'settings.logLevel': { en: 'Log level', ru: 'Уровень логов' },
  'settings.logToFile': { en: 'Log to file', ru: 'Запись в файл' },
  'settings.logToFileHint': { en: 'Save pipeline logs to disk alongside output', ru: 'Сохранять логи pipeline на диск рядом с результатами' },

  // Home — pipeline inline
  'home.scanning': { en: 'Scanning for files...', ru: 'Поиск файлов...' },
  'home.classifying': { en: 'Classifying files...', ru: 'Классификация файлов...' },
  'home.processing': { en: 'Processing...', ru: 'Обработка...' },
  'home.newFile': { en: 'New file', ru: 'Новый файл' },
  'home.done': { en: 'Done!', ru: 'Готово!' },
  'home.result': { en: 'Result', ru: 'Результат' },
  'home.openResult': { en: 'Open folder', ru: 'Открыть папку' },
  'home.analyzing': { en: 'Analyzing...', ru: 'Анализ...' },
  'home.preparingTools': { en: 'Preparing tools...', ru: 'Подготовка инструментов...' },
  'home.decompiling': { en: 'Decompiling...', ru: 'Декомпиляция...' },
  'home.error': { en: 'Error', ru: 'Ошибка' },

  // Home — stages
  'home.section.analysis': { en: 'Analysis', ru: 'Анализ' },
  'home.stage.tools': { en: 'Tools', ru: 'Инструменты' },
  'home.stage.execute': { en: 'Execute', ru: 'Декомпиляция' },
  'home.stage.done': { en: 'Done', ru: 'Готово' },
  'home.preparing': { en: 'Preparing...', ru: 'Подготовка...' },
  'home.step': { en: 'Step', ru: 'Шаг' },
  'home.file': { en: 'File', ru: 'Файл' },
  'home.filesProcessed': { en: 'files processed', ru: 'файлов обработано' },
  'home.recent': { en: 'Recent', ru: 'Последние' },
  'home.ago.minutes': { en: 'm ago', ru: 'мин назад' },
  'home.ago.hours': { en: 'h ago', ru: 'ч назад' },
  'home.ago.days': { en: 'd ago', ru: 'дн назад' },
  'home.summary': { en: 'Summary', ru: 'Итоги' },
  'home.summary.files': { en: 'files', ru: 'файлов' },
  'home.summary.decompiled': { en: 'decompiled', ru: 'декомпилировано' },
  'home.summary.skipped': { en: 'skipped', ru: 'пропущено' },
  'home.summary.log': { en: 'Full log', ru: 'Полный лог' },

  // Home — pipeline controls
  'home.cancel': { en: 'Cancel', ru: 'Отмена' },
  'home.pause': { en: 'Pause', ru: 'Пауза' },
  'home.resume': { en: 'Resume', ru: 'Продолжить' },
  'home.cancelled': { en: 'Cancelled', ru: 'Отменено' },
  'home.paused': { en: 'Paused', ru: 'Приостановлено' },

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
  'tools.checkUpdates': { en: 'Check updates', ru: 'Проверить обновления' },
  'tools.filterPlaceholder': { en: 'Filter tools...', ru: 'Фильтр инструментов...' },
  'tools.noMatch': { en: 'No tools match the filter', ru: 'Нет инструментов по фильтру' },
  'tools.ready': { en: 'Ready', ru: 'Готов' },
  'tools.pending': { en: 'Pending', ru: 'Ожидание' },

  // Settings — folder picker
  'settings.browse': { en: 'Browse...', ru: 'Обзор...' },
  'settings.githubToken': { en: 'GitHub Token (optional)', ru: 'GitHub Token (опционально)' },
  'settings.githubTokenHint': { en: 'Increases API limit from 60 to 5000 requests/hour', ru: 'Увеличивает лимит API с 60 до 5000 запросов/час' },
  'settings.createToken': { en: 'Create token', ru: 'Создать токен' },


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

  // API-triggered operations (poll-detected)
  'tools.installedViaApi': { en: 'installed', ru: 'установлен' },
  'tools.removedViaApi': { en: 'removed', ru: 'удалён' },
  'tools.updatedViaApi': { en: 'updated', ru: 'обновлён' },

  // Settings — AI Integration
  'settings.aiIntegration': { en: 'AI Integration', ru: 'Интеграция с ИИ' },
  'settings.copyInstructions': { en: 'Copy instructions for AI assistant', ru: 'Скопировать инструкции для ИИ-ассистента' },
  'settings.apiStatus': { en: 'API Status', ru: 'Статус API' },
  'settings.copyButton': { en: 'Copy AI Instructions', ru: 'Скопировать инструкции' },
  'settings.copied': { en: 'Copied!', ru: 'Скопировано!' },
  'settings.copyFailed': { en: 'Clipboard unavailable — copy manually', ru: 'Буфер недоступен — скопируйте вручную' },
  'settings.copyError': { en: 'Failed to load instructions — try again', ru: 'Не удалось загрузить инструкции — попробуйте снова' },
  'settings.close': { en: 'Close', ru: 'Закрыть' },

  // Pipeline progress
  'pipeline.toolReady': { en: 'ready', ru: 'готов' },
  'pipeline.toolInstalling': { en: 'installing...', ru: 'установка...' },
  'pipeline.toolPending': { en: 'pending', ru: 'ожидание' },
  'pipeline.downloading': { en: 'Downloading...', ru: 'Скачивание...' },
  'pipeline.extracting': { en: 'Extracting...', ru: 'Распаковка...' },
  'pipeline.recipe': { en: 'Recipe', ru: 'Рецепт' },
  'pipeline.target': { en: 'Target', ru: 'Цель' },
  'pipeline.compiler': { en: 'Compiler:', ru: 'Компилятор:' },
  'pipeline.obfuscator': { en: 'Obfuscator:', ru: 'Обфускатор:' },
  'pipeline.size': { en: 'Size:', ru: 'Размер:' },

  // Stats
  'stats.files': { en: 'files', ru: 'файлов' },
  'stats.size': { en: 'size', ru: 'объём' },
  'stats.obfuscations': { en: 'obfuscations', ru: 'обфускаций' },

  // Composition
  'composition.title': { en: 'Composition', ru: 'Состав' },
  'composition.andMore': { en: 'and {n} more', ru: 'и ещё {n}' },
  'composition.autoApply': { en: 'will apply automatically', ru: 'применится автоматически' },
  'composition.filesAffected': { en: 'files', ru: 'файлов' },
  'composition.noDeobfuscator': { en: 'Deobfuscator unavailable — results will be partial', ru: 'Деобфускатор недоступен — результат будет частичным' },
  'composition.requestSupport': { en: 'Request {name} support', ru: 'Запросить поддержку {name}' },

  // Execution
  'execution.title': { en: 'Execution', ru: 'Выполнение' },
  'execution.step': { en: 'Step', ru: 'Шаг' },
  'execution.done': { en: 'Done', ru: 'Готово' },
  'execution.skipped': { en: 'Skipped (not installed)', ru: 'Пропущен (не установлен)' },
  'execution.failed': { en: 'Failed', ru: 'Ошибка' },
  'execution.waiting': { en: 'Waiting...', ru: 'Ожидание...' },
  'execution.items': { en: 'items', ru: 'элементов' },
  'execution.decompiling': { en: 'Decompiling', ru: 'Декомпиляция' },
  'execution.processing': { en: 'Processing...', ru: 'Обработка...' },
  'ghidra:import': { en: 'Importing binary...', ru: 'Импорт бинарника...' },
  'ghidra:analyze': { en: 'Analyzing code...', ru: 'Анализ кода...' },
  'ghidra:disassemble': { en: 'Disassembling...', ru: 'Дизассемблирование...' },

  // Stepper
  'stepper.analysis': { en: 'Analysis', ru: 'Анализ' },
  'stepper.tools': { en: 'Tools', ru: 'Инструменты' },
  'stepper.execution': { en: 'Execution', ru: 'Выполнение' },
  'stepper.done': { en: 'Done', ru: 'Готово' },

  // About page
  'sidebar.about': { en: 'About', ru: 'О программе' },
  'about.title': { en: 'About Morgue', ru: 'О программе' },
  'about.description': { en: 'Automated binary decompilation pipeline for .NET, Delphi, Unity, Unreal Engine and native targets. Born from the modding community — originally built for game mod development, now a general-purpose reverse engineering toolkit.', ru: 'Автоматический pipeline декомпиляции бинарных файлов для .NET, Delphi, Unity, Unreal Engine и нативных целей. Родился из модинг-сообщества — изначально создан для разработки модов к играм, теперь универсальный инструмент реверс-инжиниринга.' },
  'about.version': { en: 'Version', ru: 'Версия' },
  'about.commit': { en: 'Commit', ru: 'Коммит' },
  'about.goVersion': { en: 'Go version', ru: 'Версия Go' },
  'about.wailsVersion': { en: 'Wails version', ru: 'Версия Wails' },
  'about.platform': { en: 'Platform', ru: 'Платформа' },
  'about.license': { en: 'License', ru: 'Лицензия' },
  'about.author': { en: 'Author', ru: 'Автор' },
  'about.links': { en: 'Links', ru: 'Ссылки' },
  'about.github': { en: 'GitHub Repository', ru: 'Репозиторий GitHub' },
  'about.issues': { en: 'Report a Bug', ru: 'Сообщить об ошибке' },
  'about.releases': { en: 'Releases', ru: 'Релизы' },
  'about.tools': { en: 'Installed tools', ru: 'Установлено инструментов' },
  'about.recipes': { en: 'Available recipes', ru: 'Доступно рецептов' },
  'about.disclaimer': { en: 'This software is provided exclusively for research and educational purposes. The user is solely responsible for ensuring that their use complies with all applicable laws and third-party rights. Commercial use is prohibited.', ru: 'Программа предоставляется исключительно для исследовательских и образовательных целей. Пользователь несёт полную ответственность за соблюдение применимого законодательства и прав третьих лиц. Коммерческое использование запрещено.' },

  // Settings — Unreal Engine
  'settings.unrealEngine': { en: 'Unreal Engine', ru: 'Unreal Engine' },
  'settings.ue5.extractPak': { en: 'Extract PAK assets', ru: 'Извлечение PAK-ассетов' },
  'settings.ue5.extractPakHint': { en: 'Extract game assets from .pak/.utoc containers. Gives access to Blueprints, data tables, and asset structure.', ru: 'Извлечение игровых ассетов из .pak/.utoc контейнеров. Даёт доступ к Blueprints, таблицам данных и структуре ассетов.' },
  'settings.ue5.sdkDump': { en: 'SDK class dump', ru: 'Дамп SDK классов' },
  'settings.ue5.sdkDumpHint': { en: 'Dump all class names, functions, properties, and inheritance. The foundation — AI uses this to understand game structure.', ru: 'Дамп всех имён классов, функций, свойств и наследования. Основа — ИИ использует это для понимания структуры игры.' },
  'settings.ue5.extractStrings': { en: 'String extraction', ru: 'Извлечение строк' },
  'settings.ue5.extractStringsHint': { en: 'Find debug strings and source file paths in the binary. Helps AI locate specific functions by name.', ru: 'Поиск отладочных строк и путей к исходным файлам в бинарнике. Помогает ИИ находить функции по имени.' },
  'settings.ue5.ghidraDecompile': { en: 'Full Ghidra decompilation', ru: 'Полная декомпиляция Ghidra' },
  'settings.ue5.ghidraDecompileHint': { en: 'Complete binary decompilation to C code. Takes hours but gives full function bodies. Enable when you need to understand HOW something works.', ru: 'Полная декомпиляция бинарника в C-код. Занимает часы, но даёт тела всех функций. Включайте когда нужно понять КАК что-то работает.' },
  'settings.ue5.nameResolution': { en: 'Name resolution', ru: 'Разрешение имён' },
  'settings.ue5.nameResolutionHint': { en: 'Replace auto-generated names (FUN_12345) with real names from SDK dump and debug strings. Critical for readability.', ru: 'Замена автосгенерированных имён (FUN_12345) на реальные из SDK-дампа и отладочных строк. Критично для читаемости.' },
  'settings.ue5.buildIndexes': { en: 'Build search indexes', ru: 'Построение индексов' },
  'settings.ue5.buildIndexesHint': { en: 'Create cross-reference indexes (who calls what, string references, class hierarchy). Enables AI to navigate the codebase instantly.', ru: 'Создание индексов перекрёстных ссылок (кто вызывает что, ссылки на строки, иерархия классов). ИИ мгновенно навигирует по коду.' },
  'settings.ue5.exportHookable': { en: 'Export hookable symbols', ru: 'Экспорт хукаемых символов' },
  'settings.ue5.exportHookableHint': { en: 'List all functions that can be hooked from Lua/UE4SS. Essential for mod development.', ru: 'Список всех функций, которые можно хукнуть из Lua/UE4SS. Необходимо для разработки модов.' },

  'settings.dotnet': { en: '.NET', ru: '.NET' },

  'settings.native': { en: 'Native', ru: 'Нативные' },
  'settings.native.ghidraDecompile': { en: 'Full Ghidra decompilation', ru: 'Полная декомпиляция Ghidra' },
  'settings.native.ghidraDecompileHint': { en: 'Decompile the native binary to C code with Ghidra. The core of native analysis but can be slow and heavy on large binaries.', ru: 'Декомпиляция нативного бинарника в C-код через Ghidra. Основа анализа нативного кода, но может быть медленной и тяжёлой на крупных бинарниках.' },

  'settings.delphi': { en: 'Delphi', ru: 'Delphi' },
  'settings.delphi.idrAnalysis': { en: 'IDR analysis', ru: 'Анализ IDR' },
  'settings.delphi.idrAnalysisHint': { en: 'Run Interactive Delphi Reconstructor to recover forms, classes, and unit names. Delphi-specific, gives the most readable output.', ru: 'Запуск Interactive Delphi Reconstructor для восстановления форм, классов и имён модулей. Специфично для Delphi, даёт наиболее читаемый результат.' },
  'settings.delphi.ghidraDecompile': { en: 'Full Ghidra decompilation', ru: 'Полная декомпиляция Ghidra' },
  'settings.delphi.ghidraDecompileHint': { en: 'Decompile the binary to C code with Ghidra as a fallback to IDR. Thorough but can be slow and heavy.', ru: 'Декомпиляция бинарника в C-код через Ghidra как дополнение к IDR. Тщательно, но может быть медленно и тяжело.' },
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
