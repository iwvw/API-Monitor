package com.iwvw.apimonitor.ui.common

import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.EnterTransition
import androidx.compose.animation.ExitTransition
import androidx.compose.animation.core.CubicBezierEasing
import androidx.compose.animation.core.Easing
import androidx.compose.animation.core.FiniteAnimationSpec
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.tween
import androidx.compose.animation.expandVertically
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.scaleIn
import androidx.compose.animation.scaleOut
import androidx.compose.animation.shrinkVertically
import androidx.compose.animation.slideInHorizontally
import androidx.compose.animation.slideOutHorizontally
import androidx.compose.animation.togetherWith
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.lazy.LazyItemScope
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.TransformOrigin
import androidx.compose.ui.graphics.graphicsLayer
import kotlin.coroutines.cancellation.CancellationException

/**
 * MD3 Expressive 运动参数。
 *
 * material3 1.4.0 的 MotionScheme 仍是 internal，Kotlin 侧取不到，
 * 因此按官方 MotionTokens 的公开数值复刻：
 * - spatial（位移）：medium2 = 400ms，让 shared axis 的位移可感知
 * - effects（透明度）：medium1 = 300ms
 * - expand/collapse：medium1 = 300ms / medium2 收窄，进入 decelerate、退出 accelerate
 */
object Motion {
    val Emphasized: Easing = CubicBezierEasing(0.2f, 0f, 0f, 1f)
    val EmphasizedDecelerate: Easing = CubicBezierEasing(0.05f, 0.7f, 0.1f, 1f)
    val EmphasizedAccelerate: Easing = CubicBezierEasing(0.3f, 0f, 0.8f, 0.15f)
    val Standard: Easing = CubicBezierEasing(0.2f, 0f, 0f, 1f)

    const val SPATIAL_MS = 400
    const val EFFECTS_MS = 300

    /** 展开：稍长，让内容「生长」出来。 */
    const val EXPAND_MS = 300

    /** 收起：略短，避免收起动作拖沓。 */
    const val COLLAPSE_MS = 240

    /** 列表项淡入淡出。 */
    const val ITEM_FADE_MS = 220

    /** 列表项位移（增删导致的重新排列）。 */
    const val ITEM_PLACEMENT_MS = 320

    fun <T> expandSpec(): FiniteAnimationSpec<T> =
        tween(durationMillis = EXPAND_MS, easing = EmphasizedDecelerate)

    fun <T> collapseSpec(): FiniteAnimationSpec<T> =
        tween(durationMillis = COLLAPSE_MS, easing = EmphasizedAccelerate)
}

/**
 * LazyColumn / LazyRow 里每一项的过渡动画：出现淡入、消失淡出、位置变化平滑位移。
 *
 * 直接放在 `items { ... }` 的 lambda 里用，因为 `animateItem` 是 [LazyItemScope] 的扩展。
 */
fun LazyItemScope.motionItem(modifier: Modifier = Modifier): Modifier =
    modifier.animateItem(
        fadeInSpec = tween(Motion.ITEM_FADE_MS, easing = Motion.Standard),
        placementSpec = tween(Motion.ITEM_PLACEMENT_MS, easing = Motion.Emphasized),
        fadeOutSpec = tween(Motion.ITEM_FADE_MS, easing = Motion.Standard),
    )

/**
 * 展开 / 收起容器。
 *
 * MD3 规范里容器高度变化用 vertical expand，配合淡入淡出，
 * 进入用 emphasized decelerate（快起慢停），退出用 emphasized accelerate（慢起快收）。
 */
@Composable
fun ExpandableContent(
    expanded: Boolean,
    modifier: Modifier = Modifier,
    content: @Composable () -> Unit,
) {
    AnimatedVisibility(
        visible = expanded,
        modifier = modifier,
        enter = expandVertically(
            animationSpec = Motion.expandSpec(),
            expandFrom = androidx.compose.ui.Alignment.Top,
        ) + fadeIn(animationSpec = tween(Motion.EXPAND_MS, easing = Motion.Standard)),
        exit = shrinkVertically(
            animationSpec = Motion.collapseSpec(),
            shrinkTowards = androidx.compose.ui.Alignment.Top,
        ) + fadeOut(animationSpec = tween(Motion.COLLAPSE_MS, easing = Motion.Standard)),
    ) {
        content()
    }
}

/**
 * MD3 共享轴（shared axis）横向切换容器。
 *
 * 底栏 Tab 切换不能用 NavHost 的 enterTransition —— 配合
 * `popUpTo(saveState) + restoreState` 时导航库走的是状态恢复，
 * 不会播放转场。这里用 [AnimatedContent] 显式驱动，保证每次切换都有动画。
 *
 * @param targetKey 当前选中项的稳定标识，变化即触发转场
 * @param order 用于判断前进/后退方向的顺序表
 */
@Composable
fun <T> SharedAxisXContent(
    targetKey: T,
    order: List<T>,
    modifier: Modifier = Modifier,
    content: @Composable (T) -> Unit,
) {
    AnimatedContent(
        targetState = targetKey,
        modifier = modifier,
        transitionSpec = {
            val from = order.indexOf(initialState)
            val to = order.indexOf(targetState)
            val forward = to >= from

            val enter = slideInHorizontally(
                animationSpec = tween(Motion.SPATIAL_MS, easing = Motion.EmphasizedDecelerate),
                initialOffsetX = { width -> if (forward) width / 4 else -width / 4 },
            ) + fadeIn(animationSpec = tween(Motion.EFFECTS_MS, easing = Motion.Standard))

            val exit = slideOutHorizontally(
                animationSpec = tween(Motion.SPATIAL_MS, easing = Motion.EmphasizedAccelerate),
                targetOffsetX = { width -> if (forward) -width / 4 else width / 4 },
            ) + fadeOut(animationSpec = tween(Motion.EFFECTS_MS, easing = Motion.Standard))

            enter togetherWith exit
        },
        label = "shared-axis-x",
        content = { target -> content(target) },
    )
}

/** 详情页/设置页这类「进入下一层」的转场：全宽横向推入。 */
@Composable
fun <T> SharedAxisXPage(
    targetKey: T,
    order: List<T>,
    modifier: Modifier = Modifier,
    content: @Composable (T) -> Unit,
) {
    AnimatedContent(
        targetState = targetKey,
        modifier = modifier,
        transitionSpec = {
            val from = order.indexOf(initialState)
            val to = order.indexOf(targetState)
            val forward = to >= from

            val enter = slideInHorizontally(
                animationSpec = tween(Motion.SPATIAL_MS, easing = Motion.EmphasizedDecelerate),
                initialOffsetX = { width -> if (forward) width / 3 else -width / 6 },
            ) + fadeIn(animationSpec = tween(Motion.EFFECTS_MS))

            val exit = slideOutHorizontally(
                animationSpec = tween(Motion.SPATIAL_MS, easing = Motion.EmphasizedAccelerate),
                targetOffsetX = { width -> if (forward) -width / 6 else width / 3 },
            ) + fadeOut(animationSpec = tween(Motion.EFFECTS_MS))

            enter togetherWith exit
        },
        label = "shared-axis-page",
        content = { target -> content(target) },
    )
}

/** Z 轴缩放：用于弹层类内容。 */
@Composable
fun <T> SharedAxisZContent(
    targetKey: T,
    modifier: Modifier = Modifier,
    content: @Composable (T) -> Unit,
) {
    AnimatedContent(
        targetState = targetKey,
        modifier = modifier,
        transitionSpec = {
            (scaleIn(
                initialScale = 0.9f,
                animationSpec = tween(Motion.SPATIAL_MS, easing = Motion.EmphasizedDecelerate),
            ) + fadeIn(tween(Motion.EFFECTS_MS))) togetherWith
                (scaleOut(
                    targetScale = 0.9f,
                    animationSpec = tween(Motion.SPATIAL_MS, easing = Motion.EmphasizedAccelerate),
                ) + fadeOut(tween(Motion.EFFECTS_MS)))
        },
        label = "shared-axis-z",
        content = { target -> content(target) },
    )
}

/**
 * 预测性返回转场（Android 14+ 手势返回，由 NavHost 内置支持）。
 *
 * 官方做法：`NavHost` 的 `popExitTransition` 里把当前页缩小 + 淡出，
 * 让用户在手势进行中就能看到「这一页正在退回上一页」。
 * 系统会把手势进度直接映射到这段动画，不需要自己接 `PredictiveBackHandler`。
 *
 * 规范取值（MD3 + Android 官方示例）：
 * - 缩放中心固定在屏幕中心
 * - 缩到 0.9，同时淡出
 * - 时长与 spatial 一致，缓动用 emphasized accelerate
 */
fun predictiveBackExit(): ExitTransition =
    scaleOut(
        targetScale = 0.9f,
        animationSpec = tween(Motion.SPATIAL_MS, easing = Motion.EmphasizedAccelerate),
        transformOrigin = TransformOrigin(pivotFractionX = 0.5f, pivotFractionY = 0.5f),
    ) + fadeOut(animationSpec = tween(Motion.EFFECTS_MS, easing = Motion.Standard))

/**
 * 预测性返回时被返回到的页面（下层）重新进入。
 *
 * 必须做 scaleIn：如果这里是 `EnterTransition.None`，手势过程中下层页完全静止，
 * 上层页却缩小淡出，观感变成「关掉一个浮层」而不是「退回上一页」。
 * 从 0.9 放回 1，和上层的 1→0.9 互补，形成明确的 Z 轴层级。
 */
fun predictiveBackEnter(): EnterTransition =
    scaleIn(
        initialScale = 0.9f,
        animationSpec = tween(Motion.SPATIAL_MS, easing = Motion.EmphasizedDecelerate),
        transformOrigin = TransformOrigin(pivotFractionX = 0.5f, pivotFractionY = 0.5f),
    ) + fadeIn(animationSpec = tween(Motion.EFFECTS_MS, easing = Motion.Standard))

/** 进入下一层的转场：从右侧推入。 */
fun pushEnter(): EnterTransition =
    slideInHorizontally(
        animationSpec = tween(Motion.SPATIAL_MS, easing = Motion.EmphasizedDecelerate),
        initialOffsetX = { width -> width / 3 },
    ) + fadeIn(animationSpec = tween(Motion.EFFECTS_MS))

/** 被新页面覆盖时：轻微左移并淡出，让层级关系明确。 */
fun pushExit(): ExitTransition =
    slideOutHorizontally(
        animationSpec = tween(Motion.SPATIAL_MS, easing = Motion.EmphasizedAccelerate),
        targetOffsetX = { width -> -width / 6 },
    ) + fadeOut(animationSpec = tween(Motion.EFFECTS_MS))

/**
 * 同层级 tab 切换的进入动画：横向小位移 + 淡入。
 *
 * 位移幅度比下钻页面小得多（1/12 vs 1/3），让「同层」与「下钻」在动效上可区分。
 */
fun tabEnter(forward: Boolean): EnterTransition =
    slideInHorizontally(
        animationSpec = tween(Motion.SPATIAL_MS, easing = Motion.EmphasizedDecelerate),
        initialOffsetX = { width -> if (forward) width / 12 else -width / 12 },
    ) + fadeIn(animationSpec = tween(Motion.EFFECTS_MS, easing = Motion.Standard))

/** 同层级 tab 切换的退出动画。 */
fun tabExit(forward: Boolean): ExitTransition =
    slideOutHorizontally(
        animationSpec = tween(Motion.SPATIAL_MS, easing = Motion.EmphasizedAccelerate),
        targetOffsetX = { width -> if (forward) -width / 12 else width / 12 },
    ) + fadeOut(animationSpec = tween(Motion.EFFECTS_MS, easing = Motion.Standard))

/** 两个 route 是否都属于同一层（底栏 tab）。 */
fun isTabToTab(fromRoute: String?, toRoute: String?, tabs: List<String>): Boolean =
    fromRoute != null && toRoute != null && fromRoute in tabs && toRoute in tabs

